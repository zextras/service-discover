// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	mocksexec "github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/spire"
)

// migrateCovFailStart makes the start of the given unit fail, ahead of the expectations of the fixture.
func migrateCovFailStart(f *migrateFixture, unit string) {
	call := f.unit.On("StartUnit", unit, "replace", mock.Anything).Return(0, errors.New("boom"))
	calls := f.unit.ExpectedCalls
	f.unit.ExpectedCalls = append([]*mock.Call{call}, calls[:len(calls)-1]...)
}

// migrateCovDir creates a non-empty directory where a file is expected, so that writing or removing it fails.
func migrateCovDir(t *testing.T, path string) {
	t.Helper()

	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.MkdirAll(filepath.Join(path, "child"), 0755))
}

func TestSetup_runMigrate(t *testing.T) {
	flags := func(format formatter.OutputFormat) *command.GlobalCommonFlags {
		return &command.GlobalCommonFlags{Format: format}
	}

	t.Run("prints the result in the requested format", func(t *testing.T) {
		for format, check := range map[formatter.OutputFormat]func(t *testing.T, out string){
			formatter.PlainFormatOutput: func(t *testing.T, out string) {
				assert.Contains(t, out, "migrated to SPIRE")
			},
			formatter.JSONFormatOutput: func(t *testing.T, out string) {
				var decoded migrateOutput
				require.NoError(t, json.Unmarshal([]byte(out), &decoded))
				assert.Contains(t, strings.Join(decoded.Steps, "\n"), "migrated to SPIRE")
			},
		} {
			f := newMigrateFixture(t, nil)
			out := &bytes.Buffer{}

			require.NoError(t, f.setup.runMigrate(flags(format), f.deps, out))
			check(t, out.String())
		}
	})

	t.Run("invalid output format", func(t *testing.T) {
		f := newMigrateFixture(t, nil)

		err := f.setup.runMigrate(flags(formatter.OutputFormat(99)), f.deps, &bytes.Buffer{})
		require.ErrorContains(t, err, "formatting option is not valid")
	})

	t.Run("consul binary missing", func(t *testing.T) {
		f := newMigrateFixture(t, nil)
		version := new(mocksexec.Cmd)
		version.On("Run").Return(errors.New("not found"))
		f.deps.ExpectedCalls = append([]*mock.Call{
			f.deps.On("CreateCommand", command.ConsulBin, "version").Return(version),
		}, f.deps.ExpectedCalls[:len(f.deps.ExpectedCalls)-1]...)

		err := f.setup.runMigrate(flags(formatter.PlainFormatOutput), f.deps, &bytes.Buffer{})
		require.ErrorContains(t, err, "unable to execute consul binary")
	})

	t.Run("not root", func(t *testing.T) {
		f := newMigrateFixture(t, nil)
		f.deps.ExpectedCalls = append([]*mock.Call{f.deps.On("GetuidSyscall").Return(1000)},
			f.deps.ExpectedCalls[:len(f.deps.ExpectedCalls)-1]...)

		err := f.setup.runMigrate(flags(formatter.PlainFormatOutput), f.deps, &bytes.Buffer{})
		require.ErrorContains(t, err, "must be executed as root")
	})

	t.Run("migration error", func(t *testing.T) {
		f := newMigrateFixture(t, nil)
		require.NoError(t, os.Remove(f.setup.ConsulFileConfig))

		err := f.setup.runMigrate(flags(formatter.PlainFormatOutput), f.deps, &bytes.Buffer{})
		require.ErrorContains(t, err, "not set up")
	})
}

func TestSetup_Run_migrateNeedsJoin(t *testing.T) {
	s := &Setup{Migrate: true}

	require.ErrorContains(t, s.Run(&command.GlobalCommonFlags{}), "--migrate needs --join")
}

func TestSetup_runJoinWith_dispatchesMigrate(t *testing.T) {
	f := newMigrateFixture(t, nil)
	out := &bytes.Buffer{}

	require.NoError(t, f.setup.runJoinWith(&command.GlobalCommonFlags{Format: formatter.PlainFormatOutput}, f.deps, out))
	assert.Contains(t, out.String(), "migrated to SPIRE")
}

func TestSetup_migrateOutput_json(t *testing.T) {
	out := &migrateOutput{}
	out.add("first")
	out.add("second")

	rendered, err := out.JSONRender()
	require.NoError(t, err)
	assert.JSONEq(t, `{"steps":["first","second"]}`, rendered)
}

func TestSetup_existingConfig(t *testing.T) {
	tests := []struct {
		name     string
		prepare  func(t *testing.T, s *Setup)
		wantKind consulConfigKind
		wantNode string
		wantErr  string
	}{
		{
			name:     "missing",
			prepare:  func(*testing.T, *Setup) {},
			wantKind: configMissing,
		},
		{
			name: "unreadable",
			prepare: func(t *testing.T, s *Setup) {
				require.NoError(t, os.MkdirAll(s.ConsulFileConfig, 0755))
			},
			wantErr: "is a directory",
		},
		{
			name: "not json",
			prepare: func(t *testing.T, s *Setup) {
				require.NoError(t, os.WriteFile(s.ConsulFileConfig, []byte("{"), 0600))
			},
			wantErr: "unable to decode",
		},
		{
			name: "server",
			prepare: func(t *testing.T, s *Setup) {
				require.NoError(t, os.WriteFile(s.ConsulFileConfig, []byte(`{"server":true,"node_name":"n"}`), 0600))
			},
			wantKind: configServer,
			wantNode: "n",
		},
		{
			name: "joined",
			prepare: func(t *testing.T, s *Setup) {
				require.NoError(t, os.WriteFile(s.ConsulFileConfig,
					[]byte(`{"auto_config":{"enabled":true},"node_name":"n"}`), 0600))
			},
			wantKind: configJoined,
			wantNode: "n",
		},
		{
			name: "tarball",
			prepare: func(t *testing.T, s *Setup) {
				require.NoError(t, os.WriteFile(s.ConsulFileConfig, []byte(tarballMain), 0600))
			},
			wantKind: configTarball,
			wantNode: testNode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Setup{ConsulFileConfig: filepath.Join(t.TempDir(), "main.json")}
			tt.prepare(t, s)

			kind, node, err := s.existingConfig()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantNode, node)
		})
	}
}

func TestSetup_migrate_failures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, f *migrateFixture)
		wantErr string
		// restored tells that the old setup must be back in place
		restored bool
	}{
		{
			name: "interrupted migration cannot be restored",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, os.WriteFile(f.setup.migrateMarkerPath(), nil, 0600))
				// removing the hosts file block fails: the hosts file is a directory
				require.NoError(t, os.Remove(f.dirs.hosts))
				require.NoError(t, os.MkdirAll(f.dirs.hosts, 0755))
			},
			wantErr: "unable to restore the configuration saved by an interrupted migration",
		},
		{
			name: "unreadable configuration",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, os.Remove(f.setup.ConsulFileConfig))
				require.NoError(t, os.MkdirAll(f.setup.ConsulFileConfig, 0755))
			},
			wantErr: "is a directory",
		},
		{
			name: "already joined and the old setup cannot be removed",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig, []byte(`{"auto_config":{"enabled":true}}`), 0600))
				migrateCovDir(t, f.setup.ClusterCredential)
			},
			wantErr: "unable to remove",
		},
		{
			name: "invalid bundle",
			prepare: func(t *testing.T, f *migrateFixture) {
				f.setup.Stdin = strings.NewReader("not a bundle")
			},
			wantErr: "bundle",
		},
		{
			name: "binding address not readable",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, os.Remove(f.setup.MutableConfigFile))
			},
			wantErr: "unable to read the binding address",
		},
		{
			name: "backup fails",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, os.RemoveAll(f.setup.ConsulData))
				require.NoError(t, os.WriteFile(f.setup.ConsulData, nil, 0600))
			},
			wantErr: "unable to back up the current configuration",
		},
		{
			name: "restart fails and the rollback fails too",
			prepare: func(t *testing.T, f *migrateFixture) {
				migrateCovFailStart(f, serviceDiscoverUnit)
			},
			wantErr: "The previous configuration could not be restored",
		},
		{
			name: "the old setup cannot be removed after the switch",
			prepare: func(t *testing.T, f *migrateFixture) {
				migrateCovDir(t, filepath.Join(f.dirs.consul, command.ConsulAgentCertificate))
			},
			wantErr: "unable to remove",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMigrateFixture(t, nil)
			tt.prepare(t, f)

			_, err := f.setup.migrate(f.deps)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSetup_checkMigrateBindAddress(t *testing.T) {
	tests := []struct {
		name    string
		bind    string
		mutable string
		wantErr string
		want    string
	}{
		{name: "given", bind: "192.168.1.1", want: "192.168.1.1"},
		{name: "read from the mutable configuration", mutable: `{"bind_addr":"192.168.1.1"}`, want: "192.168.1.1"},
		{name: "mutable configuration is not json", mutable: "{", wantErr: "unable to read the binding address"},
		{name: "mutable configuration without address", mutable: "{}", wantErr: "unable to read the binding address"},
		{name: "address not of this host", bind: "10.9.9.9", wantErr: "invalid binding address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMigrateFixture(t, nil)
			f.setup.BindAddress = tt.bind

			if tt.mutable != "" {
				require.NoError(t, os.WriteFile(f.setup.MutableConfigFile, []byte(tt.mutable), 0600))
			}

			err := f.setup.checkMigrateBindAddress(f.deps)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, f.setup.BindAddress)
		})
	}

	t.Run("interfaces cannot be listed", func(t *testing.T) {
		f := newMigrateFixture(t, nil)
		f.deps.ExpectedCalls = append([]*mock.Call{f.deps.On("NetInterfaces").Return(nil, errors.New("no interfaces"))},
			f.deps.ExpectedCalls[:len(f.deps.ExpectedCalls)-1]...)
		f.setup.BindAddress = "192.168.1.1"

		require.ErrorContains(t, f.setup.checkMigrateBindAddress(f.deps), "no interfaces")
	})
}

func TestSetup_backupForMigration_failures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, f *migrateFixture)
	}{
		{
			name: "backup directory cannot be created",
			prepare: func(t *testing.T, f *migrateFixture) {
				file := filepath.Join(t.TempDir(), "file")
				require.NoError(t, os.WriteFile(file, nil, 0600))
				f.setup.ConsulHome = file
			},
		},
		{
			name: "source cannot be read",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, os.RemoveAll(f.setup.ConsulData))
				require.NoError(t, os.WriteFile(f.setup.ConsulData, nil, 0600))
			},
		},
		{
			name: "copy cannot be written",
			prepare: func(t *testing.T, f *migrateFixture) {
				migrateCovDir(t, filepath.Join(f.setup.migrateBackupPath(), filepath.Base(f.setup.ConsulFileConfig)))
			},
		},
		{
			name: "marker cannot be written",
			prepare: func(t *testing.T, f *migrateFixture) {
				migrateCovDir(t, f.setup.migrateMarkerPath())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMigrateFixture(t, nil)
			tt.prepare(t, f)

			require.Error(t, f.setup.backupForMigration())
		})
	}
}

func TestSetup_switchToSpire_failures(t *testing.T) {
	validBundle := func() *spire.JoinBundle {
		return &spire.JoinBundle{
			SpireServer: "mail.example.com:8081", SpireBundle: "bundle", JoinToken: testJoinToken,
			ConsulCA: testCA, ConsulServers: []string{"10.0.0.1:8300"}, NodeName: testNode,
		}
	}

	tests := []struct {
		name    string
		prepare func(t *testing.T, f *migrateFixture, bundle *spire.JoinBundle)
		wantErr string
	}{
		{
			name: "invalid SPIRE server address",
			prepare: func(_ *testing.T, _ *migrateFixture, bundle *spire.JoinBundle) {
				bundle.SpireServer = "no-port"
			},
			wantErr: "invalid SPIRE server address",
		},
		{
			name: "CA cannot be written",
			prepare: func(t *testing.T, f *migrateFixture, _ *spire.JoinBundle) {
				migrateCovDir(t, filepath.Join(f.dirs.consul, command.ConsulCA))
			},
			wantErr: "unable to write",
		},
		{
			name: "SPIRE servers cannot be named in the hosts file",
			prepare: func(t *testing.T, f *migrateFixture, bundle *spire.JoinBundle) {
				dialSpireServer = func(string) error { return nil }
				bundle.ConsulServers = []string{"10.0.0.1:8300", "10.0.0.2:8300"}

				require.NoError(t, os.Remove(f.dirs.hosts))
				require.NoError(t, os.MkdirAll(f.dirs.hosts, 0755))
			},
			wantErr: "unable to map",
		},
		{
			name: "intro token cannot be written",
			prepare: func(t *testing.T, f *migrateFixture, _ *spire.JoinBundle) {
				migrateCovDir(t, filepath.Join(f.dirs.consul, introTokenFile))
			},
			wantErr: "unable to write",
		},
		{
			name: "stale auto config cannot be removed",
			prepare: func(t *testing.T, f *migrateFixture, _ *spire.JoinBundle) {
				migrateCovDir(t, filepath.Join(f.setup.ConsulData, autoConfigFile))
			},
			wantErr: "directory not empty",
		},
		{
			name: "consul configuration cannot be written",
			prepare: func(t *testing.T, f *migrateFixture, _ *spire.JoinBundle) {
				require.NoError(t, os.Remove(f.setup.ConsulFileConfig))
				require.NoError(t, os.MkdirAll(f.setup.ConsulFileConfig, 0755))
			},
			wantErr: "is a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMigrateFixture(t, nil)
			bundle := validBundle()
			tt.prepare(t, f, bundle)

			require.ErrorContains(t, f.setup.switchToSpire(f.deps, bundle), tt.wantErr)
		})
	}
}

func TestSetup_hostnameForFinalize(t *testing.T) {
	dir := t.TempDir()
	localConfig := func(name, hostname string) string {
		keys := []string{"ldap_master_url", "ldap_url", "zimbra_ldap_userdn", "zimbra_ldap_password"}
		content := "<localconfig>\n"

		for _, key := range keys {
			content += `<key name="` + key + `"><value>x</value></key>` + "\n"
		}

		content += `<key name="zimbra_server_hostname"><value>` + hostname + "</value></key>\n</localconfig>\n"
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0600))

		return path
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "from the local configuration", path: localConfig("with", "mail.example.com"), want: "mail.example.com"},
		{name: "empty hostname", path: localConfig("empty", ""), want: "<hostname of this node>"},
		{name: "no local configuration", path: filepath.Join(dir, "missing"), want: "<hostname of this node>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Setup{LocalConfigPath: tt.path}

			assert.Equal(t, tt.want, s.hostnameForFinalize())
		})
	}
}

func TestSetup_rollbackMigration_failures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, f *migrateFixture)
		wantErr string
	}{
		{
			name: "SPIRE agent cannot be removed",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, os.Remove(f.dirs.hosts))
				require.NoError(t, os.MkdirAll(f.dirs.hosts, 0755))
			},
			wantErr: "is a directory",
		},
		{
			name: "auto config cannot be removed",
			prepare: func(t *testing.T, f *migrateFixture) {
				migrateCovDir(t, filepath.Join(f.setup.ConsulData, autoConfigFile))
			},
			wantErr: "directory not empty",
		},
		{
			name: "tokens cannot be removed",
			prepare: func(t *testing.T, f *migrateFixture) {
				migrateCovDir(t, filepath.Join(f.setup.ConsulData, aclTokensFile))
			},
			wantErr: "directory not empty",
		},
		{
			name: "backup cannot be read",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, f.setup.backupForMigration())
				migrateCovDir(t, filepath.Join(f.setup.migrateBackupPath(), command.ConsulCA))
			},
			wantErr: "is a directory",
		},
		{
			name: "saved file cannot be restored",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, f.setup.backupForMigration())
				require.NoError(t, os.Remove(f.setup.ConsulFileConfig))
				migrateCovDir(t, f.setup.ConsulFileConfig)
			},
			wantErr: "unable to write",
		},
		{
			name: "Consul cannot be restarted",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, f.setup.backupForMigration())
				migrateCovFailStart(f, serviceDiscoverUnit)
			},
			wantErr: "unable to start",
		},
		{
			name: "marker is gone",
			prepare: func(t *testing.T, f *migrateFixture) {
				require.NoError(t, f.setup.backupForMigration())
				require.NoError(t, os.Remove(f.setup.migrateMarkerPath()))
			},
			wantErr: "no such file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMigrateFixture(t, nil)
			tt.prepare(t, f)

			require.ErrorContains(t, f.setup.rollbackMigration(f.deps), tt.wantErr)
		})
	}
}

func TestWaitForRejoin_reportsTheLastError(t *testing.T) {
	newMigrateFixture(t, nil)
	consulStatusLeader = func() (string, error) { return "", errors.New("connection refused") }

	err := waitForRejoin()
	require.ErrorContains(t, err, "did not rejoin the cluster")
	require.ErrorContains(t, err, "connection refused")
}

func TestSetup_migrate_nodeWithoutOptionalFiles(t *testing.T) {
	f := newMigrateFixture(t, nil)
	*f.leader = ""

	caPath := filepath.Join(f.dirs.consul, command.ConsulCA)
	tokensPath := filepath.Join(f.setup.ConsulData, aclTokensFile)

	require.NoError(t, os.Remove(caPath))
	require.NoError(t, os.Remove(tokensPath))

	_, err := f.setup.migrate(f.deps)
	require.ErrorContains(t, err, "previous configuration was restored")

	assert.Equal(t, tarballMain, string(mustReadFile(t, f.setup.ConsulFileConfig)))
	assert.NoFileExists(t, tokensPath, "what was not there is not invented by the rollback")
	assert.NoFileExists(t, f.setup.migrateMarkerPath())
}

func TestSetup_migrate_markerCannotBeRemoved(t *testing.T) {
	f := newMigrateFixture(t, nil)

	// the agent restarts: from then on the marker cannot be removed
	call := f.unit.On("StartUnit", serviceDiscoverUnit, "replace", mock.Anything).Run(func(args mock.Arguments) {
		migrateCovDir(t, f.setup.migrateMarkerPath())
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	calls := f.unit.ExpectedCalls
	f.unit.ExpectedCalls = append([]*mock.Call{call}, calls[:len(calls)-1]...)

	_, err := f.setup.migrate(f.deps)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "restored")
}
