// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
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
	dirs  struct{ consul, spire string }
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
	fixture.setup = &Setup{
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
