// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/cmd/server/config"
	mocks2 "github.com/zextras/service-discover/pkg/carbonio/mocks"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/command/setup/mocks"
	"github.com/zextras/service-discover/pkg/encrypter"
	mocks22 "github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/spire"
	"github.com/zextras/service-discover/test"
)

const (
	covSingleServerMethod = `{"Config":{"JWKSURL":"https://10.0.0.1:8082/keys"}}`
	covLeader             = "10.0.0.2:8300"
)

// covOverride makes the next calls of method with args answer ret, taking precedence over the expectations already set.
func covOverride(deps *mocks.BusinessDependencies, method string, args []any, ret ...any) {
	deps.On(method, args...).Return(ret...)

	last := len(deps.ExpectedCalls) - 1
	deps.ExpectedCalls = append([]*mock.Call{deps.ExpectedCalls[last]}, deps.ExpectedCalls[:last]...)
}

func covFailingCmd(message string) *mocks22.Cmd {
	cmd := new(mocks22.Cmd)
	cmd.On("Output").Return([]byte(""), errors.New(message))
	cmd.On("Run").Return(errors.New(message))

	return cmd
}

func covEnable(t *testing.T, f *enableFixture, secrets func() (clusterSecrets, error)) error {
	t.Helper()

	_, err := f.setup.enableSpire(f.deps, secrets)

	return err
}

func TestNewSpire_usesTheInstallationPaths(t *testing.T) {
	spireCmd := NewSpire()

	assert.Equal(t, config.ConsultFileConfig, spireCmd.Enable.setup.ConsulFileConfig)
	assert.Equal(t, config.ConsultFileConfig, spireCmd.Finalize.setup.ConsulFileConfig)
}

func TestEnableOutput_renders(t *testing.T) {
	out := &enableOutput{}
	out.add("first")
	out.add("second")

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Equal(t, "first\nsecond\n", plain)

	rendered, err := out.JSONRender()
	require.NoError(t, err)
	assert.JSONEq(t, `{"steps":["first","second"]}`, rendered)
}

func TestIsLeader(t *testing.T) {
	tests := []struct {
		name, leader, bindAddress string
		want                      bool
	}{
		{"host and port", "10.0.0.2:8300", "10.0.0.2", true},
		{"bind address with a mask", "10.0.0.2:8300", "10.0.0.2/24", true},
		{"no port", "10.0.0.2", "10.0.0.2", true},
		{"another server", "10.0.0.9:8300", "10.0.0.2", false},
		{"another server without port", "10.0.0.9", "10.0.0.2/24", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isLeader(tt.leader, tt.bindAddress))
		})
	}
}

func TestReadJSONObject_failures(t *testing.T) {
	dir := t.TempDir()

	_, err := readJSONObject(filepath.Join(dir, "missing.json"))
	require.ErrorContains(t, err, "unable to read")

	broken := filepath.Join(dir, "broken.json")
	require.NoError(t, os.WriteFile(broken, []byte("{"), 0600))

	_, err = readJSONObject(broken)
	require.ErrorContains(t, err, "unable to decode")
}

func TestToGeneric_unmarshalable(t *testing.T) {
	_, err := toGeneric(make(chan int))
	require.Error(t, err)
}

func TestSetup_readBindAddress_failures(t *testing.T) {
	tests := []struct {
		name    string
		content *string
		message string
	}{
		{"missing file", nil, "unable to read the binding address of this server"},
		{"not JSON", covPtr("nope"), "unable to read the binding address from"},
		{"no address", covPtr(`{"bind_addr":""}`), "unable to read the binding address from"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if tt.content != nil {
				require.NoError(t, os.WriteFile(path, []byte(*tt.content), 0600))
			}

			setup := Setup{MutableConfigFile: path}

			_, err := setup.readBindAddress()
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func covPtr(value string) *string { return &value }

func TestSetup_enableSpire_failures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, f *enableFixture)
		message string
	}{
		{
			"consul binary missing",
			func(_ *testing.T, f *enableFixture) {
				covOverride(f.deps, "CreateCommand", []any{consulBin, "version"}, covFailingCmd("not found"))
			},
			"unable to execute consul binary",
		},
		{
			"service-discover not set up",
			func(_ *testing.T, f *enableFixture) { f.setup.ConsulFileConfig += ".missing" },
			"service-discover is not set up",
		},
		{
			"configuration is not JSON",
			func(t *testing.T, f *enableFixture) {
				require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig, []byte("{"), 0600))
			},
			"unable to decode",
		},
		{
			"no binding address",
			func(_ *testing.T, f *enableFixture) { f.setup.MutableConfigFile += ".missing" },
			"binding address",
		},
		{
			"localconfig unreadable",
			func(_ *testing.T, f *enableFixture) { f.setup.LocalConfigPath += ".missing" },
			"",
		},
		{
			"leader lookup fails",
			func(_ *testing.T, f *enableFixture) {
				covOverride(f.deps, "ConsulLeader", nil, "", errors.New("agent down"))
			},
			"no leader",
		},
		{
			"cluster secrets unreadable",
			func(*testing.T, *enableFixture) {},
			"wrong password",
		},
		{
			"auth method read fails",
			func(_ *testing.T, f *enableFixture) {
				covOverride(f.deps, "CreateCommand", []any{consulBin, "acl", "auth-method", "read", "-name", "spire",
					"-format", "json"}, covFailingCmd("acl unavailable"))
			},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEnableFixture(t, covLeader, "")
			tt.prepare(t, f)

			secrets := secretsFor()
			if tt.name == "cluster secrets unreadable" {
				secrets = func() (clusterSecrets, error) { return clusterSecrets{}, errors.New("wrong password") }
			}

			err := covEnable(t, f, secrets)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.message)
			assert.Zero(t, *f.restarts, "Consul is not restarted when something fails before")
		})
	}
}

func TestSetup_enableSpire_ldapUnavailable(t *testing.T) {
	f := newEnableFixture(t, covLeader, "")
	ldap := new(mocks2.LdapHandler)
	ldap.On("CheckServerAvailability", mock.Anything).Return(errors.New("ldap is down"))

	covOverride(f.deps, "LdapHandler", []any{mock.Anything}, ldap)

	require.ErrorContains(t, covEnable(t, f, secretsFor()), "ldap")
}

func TestSetup_enableSpire_spireServerFailures(t *testing.T) {
	t.Run("the SPIRE configuration cannot be written", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, "")
		f.setup.spireDSN = enableTestDSN

		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, nil, 0600))
		f.setup.SpireConfigDir = filepath.Join(blocker, "spire")

		require.ErrorContains(t, covEnable(t, f, secretsFor()), "unable to set up the SPIRE server")
	})

	t.Run("the CA key cannot be restored", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, "")
		keyPath := filepath.Join(f.consulHome, command.ConsulCAKey)
		require.NoError(t, os.Remove(keyPath))
		// dangling: it is not there, and it cannot be created
		require.NoError(t, os.Symlink(filepath.Join(f.consulHome, "missing", "key"), keyPath))

		err := covEnable(t, f, func() (clusterSecrets, error) {
			return clusterSecrets{password: "pw", token: "root", caKeyPEM: []byte("key")}, nil
		})
		require.Error(t, err)
	})

	t.Run("the SPIRE agent cannot be set up", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, "")
		covOverride(f.deps, "CreateCommand", []any{spire.ServerBin, "token", "generate", "-spiffeID",
			"spiffe://carbonio/node/mail.example.com", "-ttl", "600", "-output", "json", "-socketPath",
			spire.ServerSocket}, covFailingCmd("no token"))

		require.ErrorContains(t, covEnable(t, f, secretsFor()), "unable to set up the SPIRE agent of this server")
	})

	t.Run("the SPIRE agent is already attested", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, "")
		require.NoError(t, os.MkdirAll(f.spireDir, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(f.spireDir, spire.AgentConfFile), []byte("agent"), 0600))

		out, err := f.setup.enableSpire(f.deps, secretsFor())
		require.NoError(t, err)

		plain, err := out.PlainRender()
		require.NoError(t, err)
		assert.NotContains(t, plain, "SPIRE agent of this server running")
		f.deps.AssertNotCalled(t, "CreateCommand", spire.ServerBin, "token", "generate", "-spiffeID",
			"spiffe://carbonio/node/mail.example.com", "-ttl", "600", "-output", "json", "-socketPath",
			spire.ServerSocket)
	})
}

func TestSetup_enableSpire_autoConfigFailures(t *testing.T) {
	t.Run("the Consul CA is missing", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, covSingleServerMethod)
		require.NoError(t, os.Remove(filepath.Join(f.consulHome, command.ConsulCA)))

		require.ErrorContains(t, covEnable(t, f, secretsFor()), "unable to read the Consul CA")
	})

	t.Run("the configuration cannot be saved", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write read-only files")
		}

		f := newEnableFixture(t, covLeader, covSingleServerMethod)
		require.NoError(t, os.Chmod(f.setup.ConsulFileConfig, 0400))

		require.ErrorContains(t, covEnable(t, f, secretsFor()), "unable to save the configuration file")
	})

	t.Run("the permissions cannot be restricted", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, covSingleServerMethod)
		covOverride(f.deps, "LookupUser", []any{mock.Anything}, nil, errors.New("no such user"))
		covOverride(f.deps, "LookupGroup", []any{mock.Anything}, nil, errors.New("no such group"))

		require.Error(t, covEnable(t, f, secretsFor()))
	})

	t.Run("the authorization is already configured", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, covSingleServerMethod)

		require.NoError(t, covEnable(t, f, secretsFor()))
		assert.Equal(t, 1, *f.restarts)

		require.NoError(t, covEnable(t, f, secretsFor()))
		assert.Equal(t, 1, *f.restarts, "nothing changed, nothing restarted")
	})
}

func TestSetup_enableSpire_restartAndAuthMethodFailures(t *testing.T) {
	t.Run("Consul cannot be restarted", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, covSingleServerMethod)
		covOverride(f.deps, "CreateCommand", []any{"systemctl", "restart", serviceDiscoverUnit},
			covFailingCmd("restart failed"))

		require.ErrorContains(t, covEnable(t, f, secretsFor()), "unable to restart "+serviceDiscoverUnit)
	})

	t.Run("the auth method cannot be created", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, "")
		covOverride(f.deps, "CreateCommand", []any{consulBin, "acl", "auth-method", "create", "-type", "jwt", "-name",
			"spire", "-token-locality", "local", "-config", mock.Anything}, covFailingCmd("create failed"))

		require.Error(t, covEnable(t, f, secretsFor()))
		assert.Equal(t, 1, *f.restarts, "the configuration was already changed")
	})

	t.Run("the cluster credentials cannot be updated", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, "")
		f.setup.ClusterCredential = filepath.Join(t.TempDir(), "missing", "cluster-credential")

		require.ErrorContains(t, covEnable(t, f, secretsFor()),
			"unable to add the SPIRE settings to the cluster credentials")
	})
}

func TestSetup_enableAuthMethod_missingCA(t *testing.T) {
	f := newEnableFixture(t, covLeader, "")
	require.NoError(t, os.Remove(filepath.Join(f.consulHome, command.ConsulCA)))

	err := f.setup.enableAuthMethod(f.deps, "root", "https://10.0.0.2:8082/keys", covLeader, "10.0.0.2", &enableOutput{})
	require.ErrorContains(t, err, "unable to read the Consul CA")
}

func TestSetup_restartServiceDiscover_noLeaderAfterRestart(t *testing.T) {
	f := newEnableFixture(t, covLeader, "")

	require.NoError(t, restartServiceDiscover(f.deps))
	assert.Equal(t, 1, *f.restarts)
}

func covCredential(t *testing.T, password string, files map[string]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "cluster-credential")
	out, err := os.Create(path)
	require.NoError(t, err)

	defer out.Close()

	writer, err := encrypter.NewWriter(out, []byte(password))
	require.NoError(t, err)

	for name, content := range files {
		buf, info := test.CreateDumbFile([]byte(content), name)
		require.NoError(t, writer.AddFile(buf, info, name, "/"))
	}

	require.NoError(t, writer.Flush())
	writer.Close()

	return path
}

func TestSetup_readClusterSecrets_failures(t *testing.T) {
	caPath := filepath.Join(config.ConsulHome, command.ConsulCA)[1:]
	keyPath := filepath.Join(config.ConsulHome, command.ConsulCAKey)[1:]

	t.Run("no cluster credentials", func(t *testing.T) {
		setup := Setup{ClusterCredential: filepath.Join(t.TempDir(), "missing")}

		_, err := setup.readClusterSecrets("pw")
		require.Error(t, err)
	})

	t.Run("a file is missing", func(t *testing.T) {
		setup := Setup{ClusterCredential: covCredential(t, "pw", map[string]string{caPath: "CA"})}

		_, err := setup.readClusterSecrets("pw")
		require.Error(t, err)
	})

	t.Run("the bootstrap token is not JSON", func(t *testing.T) {
		setup := Setup{ClusterCredential: covCredential(t, "pw", map[string]string{
			caPath: "CA", keyPath: "KEY", command.ConsulACLBootstrap: "not json",
		})}

		_, err := setup.readClusterSecrets("pw")
		require.ErrorContains(t, err, "unable to decode ACL Bootstrap token")
	})
}

func TestSetup_enableAuthMethod_readFails(t *testing.T) {
	f := newEnableFixture(t, covLeader, "")
	covOverride(f.deps, "CreateCommand", []any{consulBin, "acl", "auth-method", "read", "-name", "spire", "-format",
		"json"}, covFailingCmd("acl unavailable"))

	err := f.setup.enableAuthMethod(f.deps, "root", "https://10.0.0.2:8082/keys", covLeader, "10.0.0.2", &enableOutput{})
	require.Error(t, err)
	assert.Empty(t, *f.created)
}

// Inside a container the units are not managed: the SPIRE files are written and nothing is started.
func TestSetup_enableSpire_insideAContainer(t *testing.T) {
	if !command.CheckDockerContainer() {
		t.Skip("not running inside a container")
	}

	t.Run("refused", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, "")
		testingMode = false

		require.ErrorContains(t, covEnable(t, f, secretsFor()), "cannot be enabled inside a container")
	})

	t.Run("units left alone", func(t *testing.T) {
		f := newEnableFixture(t, covLeader, "")
		testingMode = false

		served, err := f.setup.enableSpireServer(f.deps, clusterSecrets{token: "root"}, "mail.example.com", "10.0.0.2",
			&enableOutput{})
		require.NoError(t, err)
		assert.Equal(t, "https://10.0.0.2:8082/keys", served)

		_, err = os.Stat(filepath.Join(f.spireDir, spire.ServerConfFile))
		require.NoError(t, err)
		f.unit.AssertNotCalled(t, "StartUnit", mock.Anything, "replace", mock.Anything)
	})
}
