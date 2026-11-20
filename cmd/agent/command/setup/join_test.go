// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/command/setup/mocks"
	mocksexec "github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/spire"
	mockssystemd "github.com/zextras/service-discover/pkg/systemd/mocks"
)

const (
	testJoinToken = "one-time-join-token"
	testJWT       = "eyJhbGciOi.payload.signature"
	testNode      = "agent-mail2-example-com"
	testCA        = "-----BEGIN CERTIFICATE-----\nCCCC\n-----END CERTIFICATE-----\n"
)

func testBundle(t *testing.T) string {
	t.Helper()

	encoded, err := (&spire.JoinBundle{
		Version:       spire.BundleVersion,
		TrustDomain:   spire.TrustDomain,
		SpireServer:   "mail.example.com:8081",
		SpireBundle:   "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n",
		JoinToken:     testJoinToken,
		ConsulCA:      testCA,
		ConsulServers: []string{"10.0.0.1:8300", "10.0.0.2:8300"},
		NodeName:      testNode,
	}).Encode()
	require.NoError(t, err)

	return encoded
}

type joinFixture struct {
	setup *Setup
	deps  *mocks.BusinessDependencies
	unit  *mockssystemd.UnitManager
	dirs  struct{ consul, spire, spireData, hosts string }
}

func newJoinFixture(t *testing.T, jwtErr error) *joinFixture {
	t.Helper()

	previousMode, previousTimeout, previousInterval := testingMode, spireStartTimeout, spirePollInterval
	testingMode, spireStartTimeout, spirePollInterval = true, 50*time.Millisecond, time.Millisecond

	t.Cleanup(func() {
		testingMode, spireStartTimeout, spirePollInterval = previousMode, previousTimeout, previousInterval
	})

	fixture := &joinFixture{}
	fixture.dirs.consul = t.TempDir()
	fixture.dirs.spire = filepath.Join(t.TempDir(), "spire")
	fixture.dirs.spireData = filepath.Join(t.TempDir(), "agent-data")
	fixture.dirs.hosts = filepath.Join(t.TempDir(), "hosts")
	require.NoError(t, os.WriteFile(fixture.dirs.hosts, []byte("127.0.0.1 localhost\n"), 0644))

	previousDial := dialSpireServer
	dialSpireServer = func(string) error { return errors.New("connection refused") }

	t.Cleanup(func() { dialSpireServer = previousDial })

	fixture.setup = &Setup{
		SpireDataDir:      fixture.dirs.spireData,
		HostsFile:         fixture.dirs.hosts,
		ConsulHome:        fixture.dirs.consul,
		ConsulData:        filepath.Join(fixture.dirs.consul, "data"),
		ConsulFileConfig:  filepath.Join(fixture.dirs.consul, "main.json"),
		MutableConfigFile: filepath.Join(fixture.dirs.consul, "config.json"),
		SpireConfigDir:    fixture.dirs.spire,
		BindAddress:       "192.168.1.1",
	}

	fixture.unit = new(mockssystemd.UnitManager)
	fixture.unit.On("StartUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	fixture.unit.On("EnableUnitFiles", mock.Anything, false, false).Return(false, nil, nil)
	fixture.unit.On("StopUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	fixture.unit.On("Close").Return()

	selected := net.Interface{Name: "eth0"}
	jwt := new(mocksexec.Cmd)

	if jwtErr != nil {
		jwt.On("Output").Return(nil, jwtErr)
	} else {
		jwt.On("Output").Return([]byte("token(spiffe://carbonio/consul/"+testNode+"):\n\t"+testJWT+"\nbundle(x):\n\t{}\n"), nil)
	}

	// Any call not listed here (LDAP, consul acl, consul tls, ...) makes the mock panic.
	fixture.deps = new(mocks.BusinessDependencies)
	fixture.deps.On("NetInterfaces").Return([]net.Interface{selected, {Name: "lo"}}, nil).
		On("AddrResolver", selected).Return([]net.Addr{&addrStub{ip: "192.168.1.1"}}, nil).
		On("SystemdUnitHandler").Return(fixture.unit, nil).
		On("LookupIP", "mail.example.com").Return([]net.IP{net.ParseIP("10.0.0.1")}, nil).
		On("GetuidSyscall").Return(0).
		On("CreateCommand", command.ConsulBin, "version").Return(okCommand()).
		On("LookupUser", mock.Anything).Return(&user.User{Uid: "10", Gid: "20"}, nil).
		On("LookupGroup", mock.Anything).Return(&user.Group{Gid: "20"}, nil).
		On("Chown", mock.AnythingOfType("string"), 10, 20).Return(nil).
		On("Chmod", mock.AnythingOfType("string"), mock.Anything).Return(nil).
		On("CreateCommand", spire.AgentBin, "api", "fetch", "jwt", "-audience", "consul-auto-config",
			"-spiffeID", "spiffe://carbonio/consul/"+testNode, "-socketPath", spire.AgentSocket).Return(jwt)

	return fixture
}

func TestSetup_join(t *testing.T) {
	fixture := newJoinFixture(t, nil)
	bundleFile := filepath.Join(t.TempDir(), "bundle")
	require.NoError(t, os.WriteFile(bundleFile, []byte(testBundle(t)+"\n"), 0600))
	fixture.setup.Join = bundleFile

	_, err := fixture.setup.join(fixture.deps)
	require.NoError(t, err)

	// SPIRE agent files: the join token only lives in agent.conf, with strict permissions
	agentConf, err := os.ReadFile(filepath.Join(fixture.dirs.spire, spire.AgentConfFile))
	require.NoError(t, err)
	assert.Contains(t, string(agentConf), `join_token = "`+testJoinToken+`"`)
	assert.Contains(t, string(agentConf), `server_address = "mail.example.com"`)
	assert.Contains(t, string(agentConf), `server_port = "8081"`)

	info, err := os.Stat(filepath.Join(fixture.dirs.spire, spire.AgentConfFile))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	trust, err := os.ReadFile(filepath.Join(fixture.dirs.spire, spire.BundleFile))
	require.NoError(t, err)
	assert.Contains(t, string(trust), "AAAA")

	// intro token and CA
	intro, err := os.ReadFile(filepath.Join(fixture.dirs.consul, "intro-token"))
	require.NoError(t, err)
	assert.Equal(t, testJWT, string(intro))

	info, err = os.Stat(filepath.Join(fixture.dirs.consul, "intro-token"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	ca, err := os.ReadFile(filepath.Join(fixture.dirs.consul, "consul-agent-ca.pem"))
	require.NoError(t, err)
	assert.Equal(t, testCA, string(ca))

	// Consul client configuration: auto_config, no gossip key, no ACL token, no certificate
	rawConfig, err := os.ReadFile(fixture.setup.ConsulFileConfig)
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, json.Unmarshal(rawConfig, &config))
	assert.Equal(t, map[string]any{
		"enabled":          true,
		"intro_token_file": filepath.Join(fixture.dirs.consul, "intro-token"),
		"server_addresses": []any{"10.0.0.1:8300", "10.0.0.2:8300"},
	}, config["auto_config"])
	assert.Equal(t, testNode, config["node_name"])
	assert.Equal(t, false, config["server"])
	assert.Equal(t, map[string]any{
		"defaults":     map[string]any{"ca_file": filepath.Join(fixture.dirs.consul, "consul-agent-ca.pem"), "verify_outgoing": true},
		"internal_rpc": map[string]any{"verify_server_hostname": true},
	}, config["tls"])
	assert.NotContains(t, config, "encrypt")
	assert.NotContains(t, config, "acl")
	assert.NotContains(t, string(rawConfig), testJoinToken)

	mutable, err := os.ReadFile(fixture.setup.MutableConfigFile)
	require.NoError(t, err)
	assert.Contains(t, string(mutable), "192.168.1.1")

	for _, unit := range []string{spire.AgentUnit, serviceDiscoverUnit} {
		fixture.unit.AssertCalled(t, "StartUnit", unit, "replace", mock.Anything)
		fixture.unit.AssertCalled(t, "EnableUnitFiles", []string{unit}, false, false)
	}

	// The join token must never show up in the arguments of a command
	for _, call := range fixture.deps.Calls {
		if call.Method != "CreateCommand" {
			continue
		}

		for _, arg := range call.Arguments {
			assert.NotContains(t, arg, testJoinToken)
		}
	}
}

func TestSetup_joinFromStdin(t *testing.T) {
	fixture := newJoinFixture(t, nil)
	fixture.setup.Join = "-"
	fixture.setup.Stdin = strings.NewReader(testBundle(t))

	_, err := fixture.setup.join(fixture.deps)
	require.NoError(t, err)
	assert.FileExists(t, fixture.setup.ConsulFileConfig)
}

func TestSetup_joinInvalidBundleChangesNothing(t *testing.T) {
	fixture := newJoinFixture(t, nil)
	fixture.setup.Join = "-"
	fixture.setup.Stdin = strings.NewReader("not-a-bundle!")

	_, err := fixture.setup.join(fixture.deps)
	require.ErrorContains(t, err, "invalid join bundle")

	entries, err := os.ReadDir(fixture.dirs.consul)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.NoDirExists(t, fixture.dirs.spire)
}

func TestSetup_joinMissingBundleFile(t *testing.T) {
	fixture := newJoinFixture(t, nil)
	fixture.setup.Join = filepath.Join(t.TempDir(), "missing")

	_, err := fixture.setup.join(fixture.deps)
	require.ErrorContains(t, err, "unable to read the join bundle")
}

func TestSetup_joinInvalidBindAddress(t *testing.T) {
	fixture := newJoinFixture(t, nil)
	fixture.setup.Join = "-"
	fixture.setup.BindAddress = "172.16.0.9"

	_, err := fixture.setup.join(fixture.deps)
	require.ErrorContains(t, err, "invalid binding address")
}

func TestSetup_joinSpireNotReady(t *testing.T) {
	fixture := newJoinFixture(t, errors.New("connection refused"))
	fixture.setup.Join = "-"
	fixture.setup.Stdin = strings.NewReader(testBundle(t))

	_, err := fixture.setup.join(fixture.deps)
	require.ErrorContains(t, err, "Workload API is not ready")
	require.ErrorContains(t, err, "valid only once")

	// Consul has not been configured nor started
	assert.NoFileExists(t, fixture.setup.ConsulFileConfig)
	fixture.unit.AssertNotCalled(t, "StartUnit", serviceDiscoverUnit, "replace", mock.Anything)
}

func TestSetup_splitServerAddress(t *testing.T) {
	host, port, err := splitServerAddress("mail.example.com:8081")
	require.NoError(t, err)
	assert.Equal(t, "mail.example.com", host)
	assert.Equal(t, 8081, port)

	host, port, err = splitServerAddress("[fe80::1]:9000")
	require.NoError(t, err)
	assert.Equal(t, "fe80::1", host)
	assert.Equal(t, 9000, port)

	for _, bad := range []string{"mail.example.com", "mail.example.com:abc", "mail.example.com:0", "mail:70000"} {
		_, _, err = splitServerAddress(bad)
		require.Error(t, err, bad)
	}
}

func TestSetup_runJoinRejectsConflictingFlags(t *testing.T) {
	t.Parallel()

	flags := &command.GlobalCommonFlags{}

	err := (&Setup{Join: "-", Password: "secret", BindAddress: "10.0.0.1"}).runJoin(flags)
	require.ErrorContains(t, err, "cannot be combined")

	err = (&Setup{Join: "-", Wizard: true, BindAddress: "10.0.0.1"}).runJoin(flags)
	require.ErrorContains(t, err, "cannot be combined")

	err = (&Setup{Join: "-"}).runJoin(flags)
	require.ErrorContains(t, err, "binding address is required")
}

func (f *joinFixture) failUnit(operation, unit string) {
	f.unit.ExpectedCalls = nil

	if operation == "start" {
		f.unit.On("StartUnit", unit, "replace", mock.Anything).Return(0, errors.New("dbus down"))
	}

	f.unit.On("StartUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)

	if operation == "enable" {
		f.unit.On("EnableUnitFiles", []string{unit}, false, false).Return(false, nil, errors.New("dbus down"))
	}

	f.unit.On("EnableUnitFiles", mock.Anything, false, false).Return(false, nil, nil)
	f.unit.On("StopUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	f.unit.On("Close").Return()
}

func TestSetup_joinFailures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, f *joinFixture)
		bundle  func(t *testing.T) string
		wantErr string
	}{
		{name: "invalid SPIRE server address", bundle: func(t *testing.T) string {
			t.Helper()

			encoded, err := (&spire.JoinBundle{
				Version: spire.BundleVersion, TrustDomain: spire.TrustDomain, SpireServer: "no-port",
				SpireBundle: "b", JoinToken: testJoinToken, ConsulCA: testCA,
				ConsulServers: []string{"10.0.0.1:8300"}, NodeName: testNode,
			}).Encode()
			require.NoError(t, err)

			return encoded
		}, wantErr: "invalid SPIRE server address"},
		{name: "Consul CA cannot be written", prepare: func(t *testing.T, f *joinFixture) {
			t.Helper()
			require.NoError(t, os.Mkdir(filepath.Join(f.setup.ConsulHome, "consul-agent-ca.pem"), 0o700))
		}, wantErr: "unable to write"},
		{name: "bind address cannot be saved", prepare: func(_ *testing.T, f *joinFixture) {
			f.setup.MutableConfigFile = filepath.Join(f.setup.ConsulHome, "missing", "config.json")
		}, wantErr: "missing"},
		{name: "SPIRE configuration cannot be written", prepare: func(t *testing.T, f *joinFixture) {
			t.Helper()
			require.NoError(t, os.WriteFile(f.setup.SpireConfigDir, nil, 0o600))
		}, wantErr: "unable to create"},
		{name: "SPIRE agent does not start", prepare: func(_ *testing.T, f *joinFixture) {
			f.failUnit("start", spire.AgentUnit)
		}, wantErr: "unable to start " + spire.AgentUnit},
		{name: "SPIRE agent cannot be enabled", prepare: func(_ *testing.T, f *joinFixture) {
			f.failUnit("enable", spire.AgentUnit)
		}, wantErr: "unable to enable " + spire.AgentUnit},
		{name: "intro token cannot be written", prepare: func(t *testing.T, f *joinFixture) {
			t.Helper()
			require.NoError(t, os.Mkdir(filepath.Join(f.setup.ConsulHome, introTokenFile), 0o700))
		}, wantErr: "unable to write"},
		{name: "Consul configuration cannot be written", prepare: func(_ *testing.T, f *joinFixture) {
			f.setup.ConsulFileConfig = filepath.Join(f.setup.ConsulHome, "missing", "main.json")
		}, wantErr: "unable to save generated configuration file"},
		{name: "service does not start", prepare: func(_ *testing.T, f *joinFixture) {
			f.failUnit("start", serviceDiscoverUnit)
		}, wantErr: "unable to start " + serviceDiscoverUnit},
		{name: "service cannot be enabled", prepare: func(_ *testing.T, f *joinFixture) {
			f.failUnit("enable", serviceDiscoverUnit)
		}, wantErr: "unable to enable " + serviceDiscoverUnit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newJoinFixture(t, nil)
			fixture.setup.Join = "-"

			bundle := testBundle
			if tt.bundle != nil {
				bundle = tt.bundle
			}

			fixture.setup.Stdin = strings.NewReader(bundle(t))

			if tt.prepare != nil {
				tt.prepare(t, fixture)
			}

			_, err := fixture.setup.join(fixture.deps)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSetup_runJoinWith(t *testing.T) {
	prepare := func(t *testing.T, uid int, consulErr error) (*joinFixture, *bytes.Buffer) {
		t.Helper()

		fixture := newJoinFixture(t, nil)
		fixture.setup.Join = "-"
		fixture.setup.Stdin = strings.NewReader(testBundle(t))

		version := new(mocksexec.Cmd)
		version.On("Run").Return(consulErr)

		kept := fixture.deps.ExpectedCalls[:0]

		for _, call := range fixture.deps.ExpectedCalls {
			if call.Method == "GetuidSyscall" || (call.Method == "CreateCommand" && call.Arguments[0] == command.ConsulBin) {
				continue
			}

			kept = append(kept, call)
		}

		fixture.deps.ExpectedCalls = kept
		fixture.deps.On("CreateCommand", command.ConsulBin, "version").Return(version).
			On("GetuidSyscall").Return(uid)

		return fixture, &bytes.Buffer{}
	}

	t.Run("joins the cluster and prints the result", func(t *testing.T) {
		fixture, out := prepare(t, 0, nil)

		require.NoError(t, fixture.setup.runJoinWith(&command.GlobalCommonFlags{Format: formatter.JSONFormatOutput}, fixture.deps, out))

		assert.FileExists(t, fixture.setup.ConsulFileConfig)
	})

	t.Run("requires the consul binary", func(t *testing.T) {
		fixture, out := prepare(t, 0, errors.New("not found"))

		err := fixture.setup.runJoinWith(&command.GlobalCommonFlags{}, fixture.deps, out)

		assert.ErrorContains(t, err, "unable to execute consul binary")
	})

	t.Run("requires root", func(t *testing.T) {
		fixture, out := prepare(t, 1000, nil)

		err := fixture.setup.runJoinWith(&command.GlobalCommonFlags{}, fixture.deps, out)

		assert.ErrorContains(t, err, "must be executed as root")
	})

	t.Run("reports the join failures", func(t *testing.T) {
		fixture, out := prepare(t, 0, nil)
		fixture.setup.Stdin = strings.NewReader("not-a-bundle!")

		err := fixture.setup.runJoinWith(&command.GlobalCommonFlags{}, fixture.deps, out)

		assert.ErrorContains(t, err, "invalid join bundle")
		assert.Empty(t, out.String())
	})
}

func TestSetup_runDelegatesToJoin(t *testing.T) {
	t.Parallel()

	err := (&Setup{Join: "-", Password: "secret"}).Run(&command.GlobalCommonFlags{})

	require.ErrorContains(t, err, "cannot be combined")
}

func TestNew_wiresTheJoinInputs(t *testing.T) {
	t.Parallel()

	setup := New()

	assert.Equal(t, spire.ConfigDir, setup.SpireConfigDir)
	assert.Equal(t, os.Stdin, setup.Stdin)
}

func okCommand() *mocksexec.Cmd {
	cmd := new(mocksexec.Cmd)
	cmd.On("Run").Return(nil)

	return cmd
}

func writeJoinBundle(t *testing.T, f *joinFixture) {
	t.Helper()

	f.setup.Join = "-"
	f.setup.Stdin = strings.NewReader(testBundle(t))
}

func TestSetup_join_spireServersHighAvailability(t *testing.T) {
	f := newJoinFixture(t, nil)
	writeJoinBundle(t, f)

	dialled := map[string]bool{}

	dialSpireServer = func(address string) error {
		dialled[address] = true

		if address == "10.0.0.3:8081" {
			return errors.New("connection refused")
		}

		return nil
	}

	// the SPIRE servers run on the Consul servers: the third, in the bundle, does not answer
	f.deps.On("LookupIP", "mail.example.com").Return([]net.IP{net.ParseIP("10.0.0.1")}, nil).Maybe()

	_, err := f.setup.join(f.deps)
	require.NoError(t, err)

	assert.True(t, dialled["10.0.0.1:8081"] && dialled["10.0.0.2:8081"])

	agentConf, err := os.ReadFile(filepath.Join(f.dirs.spire, spire.AgentConfFile))
	require.NoError(t, err)
	assert.Contains(t, string(agentConf), `server_address = "spire-server.carbonio"`)

	hosts, err := os.ReadFile(f.dirs.hosts)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1 localhost\n"+
		"# BEGIN service-discover spire servers\n"+
		"10.0.0.1 spire-server.carbonio\n"+
		"10.0.0.2 spire-server.carbonio\n"+
		"# END service-discover spire servers\n", string(hosts))
}

func TestSetup_join_singleSpireServerKeepsTheBundleHost(t *testing.T) {
	f := newJoinFixture(t, nil)
	writeJoinBundle(t, f)

	dialSpireServer = func(address string) error {
		if address == "10.0.0.1:8081" {
			return nil
		}

		return errors.New("connection refused")
	}

	_, err := f.setup.join(f.deps)
	require.NoError(t, err)

	agentConf, err := os.ReadFile(filepath.Join(f.dirs.spire, spire.AgentConfFile))
	require.NoError(t, err)
	assert.Contains(t, string(agentConf), `server_address = "mail.example.com"`)

	hosts, err := os.ReadFile(f.dirs.hosts)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1 localhost\n", string(hosts))
}

func TestHostsBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	require.NoError(t, os.WriteFile(path, []byte("127.0.0.1 localhost\n::1 localhost"), 0644))

	require.NoError(t, writeHostsBlock(path, "alias", []string{"10.0.0.1", "10.0.0.2"}))
	require.NoError(t, writeHostsBlock(path, "alias", []string{"10.0.0.3", "10.0.0.4"}), "replaces the block")

	hosts, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1 localhost\n::1 localhost\n"+hostsBegin+"\n10.0.0.3 alias\n10.0.0.4 alias\n"+hostsEnd+"\n",
		string(hosts))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm())

	require.NoError(t, removeHostsBlock(path))

	hosts, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1 localhost\n::1 localhost\n", string(hosts))

	require.NoError(t, removeHostsBlock(filepath.Join(t.TempDir(), "missing")))
}

func TestSetup_join_recoversFromAFailedAttempt(t *testing.T) {
	failed := newJoinFixture(t, errors.New("connection refused"))
	writeJoinBundle(t, failed)

	dialSpireServer = func(string) error { return nil }

	_, err := failed.setup.join(failed.deps)
	require.ErrorContains(t, err, "valid only once")

	// nothing of the attempt is left behind
	for _, path := range []string{
		filepath.Join(failed.dirs.spire, spire.AgentConfFile),
		filepath.Join(failed.dirs.spire, spire.BundleFile),
		filepath.Join(failed.dirs.consul, "consul-agent-ca.pem"),
		failed.setup.joinPendingPath(),
	} {
		assert.NoFileExists(t, path)
	}

	hosts, err := os.ReadFile(failed.dirs.hosts)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1 localhost\n", string(hosts))

	failed.unit.AssertCalled(t, "StopUnit", spire.AgentUnit, "replace", mock.Anything)

	// new bundle, same host
	retry := newJoinFixture(t, nil)
	retry.setup.ConsulHome, retry.setup.ConsulData = failed.dirs.consul, failed.setup.ConsulData
	retry.setup.ConsulFileConfig, retry.setup.MutableConfigFile = failed.setup.ConsulFileConfig, failed.setup.MutableConfigFile
	retry.setup.SpireConfigDir, retry.setup.SpireDataDir = failed.dirs.spire, failed.dirs.spireData
	retry.setup.HostsFile = failed.dirs.hosts
	writeJoinBundle(t, retry)

	_, err = retry.setup.join(retry.deps)
	require.NoError(t, err)
	assert.FileExists(t, retry.setup.ConsulFileConfig)
	assert.NoFileExists(t, retry.setup.joinPendingPath())
}

func TestSetup_join_cleansWhatAnInterruptedAttemptLeft(t *testing.T) {
	f := newJoinFixture(t, nil)
	writeJoinBundle(t, f)

	// an attempt killed half way: the Consul client configuration was written, Consul started, agent state exists
	require.NoError(t, os.MkdirAll(f.setup.ConsulData, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(f.dirs.spireData, "svid"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(f.dirs.spireData, "svid", "old"), []byte("x"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(f.setup.ConsulData, "node-id"), []byte("old"), 0600))
	require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig, []byte(`{"auto_config":{"enabled":true}}`), 0600))
	require.NoError(t, os.WriteFile(f.setup.joinPendingPath(), nil, 0600))
	require.NoError(t, writeHostsBlock(f.dirs.hosts, "old", []string{"10.9.9.9"}))

	require.NoError(t, f.setup.joinPreRun(f.deps), "the half-written join must not block the retry")

	_, err := f.setup.join(f.deps)
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(f.dirs.spireData, "svid", "old"))
	assert.NoFileExists(t, filepath.Join(f.setup.ConsulData, "node-id"))

	hosts, err := os.ReadFile(f.dirs.hosts)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1 localhost\n", string(hosts))
	assert.NotContains(t, string(hosts), "10.9.9.9")
}

func TestSetup_join_neverTouchesAClusterCredentialsSetup(t *testing.T) {
	f := newJoinFixture(t, nil)
	writeJoinBundle(t, f)

	tarballConfig := `{"encrypt":"gossip","node_name":"agent-mail2"}`
	require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig, []byte(tarballConfig), 0600))

	// no marker: refused
	require.ErrorContains(t, f.setup.joinPreRun(f.deps), "already performed")

	// a stale marker next to a configuration that is not a join one: still refused, nothing deleted
	require.NoError(t, os.WriteFile(f.setup.joinPendingPath(), nil, 0600))
	require.ErrorContains(t, f.setup.joinPreRun(f.deps), "already performed")

	content, err := os.ReadFile(f.setup.ConsulFileConfig)
	require.NoError(t, err)
	assert.Equal(t, tarballConfig, string(content))
}
