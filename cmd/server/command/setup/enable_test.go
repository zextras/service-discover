// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	mocks2 "github.com/zextras/service-discover/pkg/carbonio/mocks"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/command/setup/mocks"
	"github.com/zextras/service-discover/pkg/encrypter"
	mocks22 "github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/spire"
	mocks3 "github.com/zextras/service-discover/pkg/systemd/mocks"
	"github.com/zextras/service-discover/test"
)

const enableTestDSN = "postgres://spire:pw@db.example.com:5432/spire?sslmode=verify-full"

type enableFixture struct {
	setup      Setup
	deps       *mocks.BusinessDependencies
	unit       *mocks3.UnitManager
	consulHome string
	spireDir   string
	caPEM      string
	created    *[]string
	restarts   *int
	ldapCopy   *[]byte
	uploads    *int
}

// newEnableFixture builds a server set up before SPIRE existed. methodJSON is what Consul answers when the auth
// method is read: empty for a method that does not exist.
func newEnableFixture(t *testing.T, leader, methodJSON string) *enableFixture {
	t.Helper()

	previous := testingMode
	testingMode = true

	t.Cleanup(func() { testingMode = previous })

	waitJWKS = func(string, []byte, time.Duration, time.Duration) error { return nil }

	t.Cleanup(func() { waitJWKS = spire.WaitJWKS })

	consulHome := t.TempDir()
	caPEM := writeTestCA(t, consulHome)

	mainJSON := filepath.Join(consulHome, "main.json")
	require.NoError(t, os.WriteFile(mainJSON, []byte(`{"server":true,"node_name":"server-mail","encrypt":"k"}`), 0600))

	mutable := filepath.Join(consulHome, "config.json")
	require.NoError(t, os.WriteFile(mutable, []byte(`{"bind_addr":"10.0.0.2"}`), 0600))

	localConfig := test.GenerateRandomFile("enable")
	require.NoError(t, os.WriteFile(localConfig.Name(), test.GenerateLocalConfig(
		t, "mail.example.com", "ldap://mail.example.com:389", "ldap://mail.example.com:389",
		test.DefaultLdapUserDN, "password"), 0600))
	t.Cleanup(func() { os.Remove(localConfig.Name()) })

	spireDir := filepath.Join(t.TempDir(), "spire")

	unit := new(mocks3.UnitManager)
	unit.On("StartUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	unit.On("EnableUnitFiles", mock.Anything, false, false).Return(false, nil, nil)
	unit.On("Close").Return()

	credential := filepath.Join(t.TempDir(), "cluster-credential")
	require.NoError(t, writeCredentialEntries(credential, "pw", []credentialEntry{
		{name: command.GossipKey, mode: 0600, modTime: time.Now(), content: []byte("k")},
		{name: "ca", mode: 0600, modTime: time.Now(), content: []byte("ca")},
	}))

	initial := mustRead(t, credential)
	ldapCopy, uploads := &initial, new(int)

	ldap := new(mocks2.LdapHandler)
	ldap.On("CheckServerAvailability", mock.Anything).Return(nil)
	ldap.On("DownloadBinary", carbonio.LdapConfigBaseDn, carbonio.AttrCarbonioCredentials).
		Return(func(string, string) []byte { return *ldapCopy }, nil)
	ldap.On("UploadBinary", mock.Anything, carbonio.LdapConfigBaseDn, carbonio.AttrCarbonioCredentials).
		Run(func(args mock.Arguments) {
			content, err := io.ReadAll(args.Get(0).(io.Reader))
			require.NoError(t, err)

			*ldapCopy = content
			*uploads++
		}).Return(nil)

	created, restarts := &[]string{}, new(int)
	output := func(out string, err error) *mocks22.Cmd {
		cmd := new(mocks22.Cmd)
		cmd.On("Output").Return([]byte(out), err)
		cmd.On("Run").Return(err)

		return cmd
	}

	deps := new(mocks.BusinessDependencies)
	mockOwnership(deps)
	deps.On("GetuidSyscall").Return(0).
		On("LdapHandler", mock.Anything).Return(ldap).
		On("ConsulLeader").Return(leader, nil).
		On("SystemdUnitHandler").Return(unit, nil).
		On("CreateCommand", consulBin, "version").Return(output("", nil)).
		On("CreateCommand", "systemctl", "restart", serviceDiscoverUnit).Run(func(mock.Arguments) { *restarts++ }).
		Return(output("", nil)).
		On("CreateCommand", spire.ServerBin, "healthcheck", "-socketPath", spire.ServerSocket).
		Return(output("healthy", nil)).
		On("CreateCommand", spire.ServerBin, "bundle", "show", "-socketPath", spire.ServerSocket).
		Return(output("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n", nil)).
		On("CreateCommand", spire.ServerBin, "token", "generate", "-spiffeID", "spiffe://carbonio/node/mail.example.com",
			"-ttl", "600", "-output", "json", "-socketPath", spire.ServerSocket).
		Return(output(`{"value":"join-me"}`, nil))

	read := output(methodJSON, nil)
	if methodJSON == "" {
		read = output("", &osexec.ExitError{Stderr: []byte(`Auth method not found with name "spire"`)})
	}

	deps.On("CreateCommand", consulBin, "acl", "auth-method", "read", "-name", "spire", "-format", "json").Return(read).
		On("CreateCommand", consulBin, "acl", "auth-method", "read", "-name", "spire").Return(read)

	deps.On("CreateCommand", consulBin, "acl", "auth-method", "create", "-type", "jwt", "-name", "spire",
		"-token-locality", "local", "-config", mock.Anything).
		Run(func(args mock.Arguments) { *created = append(*created, "create:"+args.String(11)) }).
		Return(output("", nil)).Maybe()
	deps.On("CreateCommand", consulBin, "acl", "auth-method", "update", "-name", "spire", "-config", mock.Anything).
		Run(func(args mock.Arguments) { *created = append(*created, "update:"+args.String(7)) }).
		Return(output("", nil)).Maybe()

	return &enableFixture{
		setup: Setup{
			ConsulHome: consulHome, ConsulFileConfig: mainJSON, MutableConfigFile: mutable,
			LocalConfigPath: localConfig.Name(), SpireConfigDir: spireDir, ClusterCredential: credential,
		},
		deps: deps, unit: unit, consulHome: consulHome, spireDir: spireDir, caPEM: caPEM,
		created: created, restarts: restarts, ldapCopy: ldapCopy, uploads: uploads,
	}
}

func secretsFor() func() (clusterSecrets, error) {
	return func() (clusterSecrets, error) { return clusterSecrets{password: "pw", token: "root"}, nil }
}

func readMain(t *testing.T, f *enableFixture) map[string]any {
	t.Helper()

	got, err := readJSONObject(f.setup.ConsulFileConfig)
	require.NoError(t, err)

	return got
}

func TestSetup_enableSpire_caKeyFromTheClusterCredentials(t *testing.T) {
	f := newEnableFixture(t, "10.0.0.2:8300", "")
	f.setup.spireDSN = enableTestDSN

	// The CA key is on the first server only: the others get it from the cluster credentials, for the time of the
	// setup.
	keyPath := filepath.Join(f.consulHome, command.ConsulCAKey)
	key := mustRead(t, keyPath)
	require.NoError(t, os.Remove(keyPath))

	signed := false

	waitJWKS = func(string, []byte, time.Duration, time.Duration) error {
		_, err := os.Stat(keyPath)
		signed = err == nil

		return nil
	}

	_, err := f.setup.enableSpire(f.deps, func() (clusterSecrets, error) {
		return clusterSecrets{password: "pw", token: "root", caKeyPEM: key}, nil
	})
	require.NoError(t, err)
	assert.True(t, signed, "the key was available while setting SPIRE up")

	_, err = os.Stat(keyPath)
	require.Error(t, err, "it must not stay on the server")

	f2 := newEnableFixture(t, "10.0.0.2:8300", "")
	require.NoError(t, os.Remove(filepath.Join(f2.consulHome, command.ConsulCAKey)))

	_, err = f2.setup.enableSpire(f2.deps, secretsFor())
	require.ErrorContains(t, err, "CA key")
}

func TestSetup_enableSpire_firstRunThenIdempotent(t *testing.T) {
	f := newEnableFixture(t, "10.0.0.2:8300", "")
	f.setup.spireDSN = enableTestDSN

	out, err := f.setup.enableSpire(f.deps, secretsFor())
	require.NoError(t, err)

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "SPIRE is enabled on this server")

	serverConf, err := os.ReadFile(filepath.Join(f.spireDir, spire.ServerConfFile))
	require.NoError(t, err)
	assert.Contains(t, string(serverConf), `database_type = "postgres"`)

	_, err = os.Stat(filepath.Join(f.spireDir, spire.AgentConfFile))
	require.NoError(t, err, "the local SPIRE agent is set up")

	main := readMain(t, f)
	assert.Equal(t, "server-mail", main["node_name"], "the rest of the configuration is kept")
	assert.Equal(t, "k", main["encrypt"])

	static := main["auto_config"].(map[string]any)["authorization"].(map[string]any)["static"].(map[string]any)
	assert.Equal(t, spire.LocalJWKSURL, static["jwks_url"])
	assert.Equal(t, spire.Issuer, static["bound_issuer"])
	assert.Equal(t, f.caPEM, static["jwks_ca_cert"])

	assert.Equal(t, 1, *f.restarts, "Consul has to be restarted to load the authorization")
	require.Len(t, *f.created, 1)
	assert.Contains(t, (*f.created)[0], "create:")
	assert.Contains(t, (*f.created)[0], spire.LocalJWKSURL)

	// The cluster credentials, in LDAP and on this server, carry the SPIRE settings and keep everything else.
	assert.Equal(t, 1, *f.uploads)
	assert.Equal(t, *f.ldapCopy, mustRead(t, f.setup.ClusterCredential))

	published := credentialsFrom(t, f)
	assert.Equal(t, spire.LocalJWKSURL, published[spire.ClusterJWKSURLFile])
	assert.Equal(t, "k", published[command.GossipKey])
	assert.Equal(t, "ca", published["ca"])

	// Second run, with the auth method already there: nothing to do, nothing restarted.
	methodJSON := `{"Config":{"JWKSURL":"` + spire.LocalJWKSURL + `"}}`
	g := newEnableFixture(t, "10.0.0.2:8300", methodJSON)
	g.setup.spireDSN = enableTestDSN
	g.setup.ConsulFileConfig = f.setup.ConsulFileConfig
	g.setup.SpireConfigDir = f.spireDir
	g.setup.ConsulHome = f.consulHome
	*g.ldapCopy = *f.ldapCopy

	_, err = g.setup.enableSpire(g.deps, secretsFor())
	require.NoError(t, err)
	assert.Equal(t, 0, *g.restarts)
	assert.Empty(t, *g.created)
	assert.Equal(t, 0, *g.uploads, "the cluster credentials are already up to date")
	g.deps.AssertNotCalled(t, "CreateCommand", spire.ServerBin, "token", "generate", "-spiffeID",
		"spiffe://carbonio/node/mail.example.com", "-ttl", "600", "-output", "json", "-socketPath", spire.ServerSocket)
}

func TestSetup_enableSpire_followerLeavesTheAuthMethodToTheLeader(t *testing.T) {
	f := newEnableFixture(t, "10.0.0.9:8300", "")
	f.setup.spireDSN = enableTestDSN

	out, err := f.setup.enableSpire(f.deps, secretsFor())
	require.NoError(t, err)

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "Run this command again on the leader")
	assert.Empty(t, *f.created, "Consul would fetch the keys from the leader, that may not run SPIRE yet")
	assert.Equal(t, 1, *f.restarts)
}

func TestSetup_enableSpire_refusals(t *testing.T) {
	t.Run("no leader", func(t *testing.T) {
		f := newEnableFixture(t, "", "")

		_, err := f.setup.enableSpire(f.deps, secretsFor())
		require.ErrorContains(t, err, "no leader")

		_, statErr := os.Stat(filepath.Join(f.spireDir, spire.ServerConfFile))
		require.Error(t, statErr, "nothing is touched")
		assert.Equal(t, 0, *f.restarts)
	})

	t.Run("high availability cluster, no datastore", func(t *testing.T) {
		f := newEnableFixture(t, "10.0.0.2:8300", `{"Config":{"JWKSURL":"`+spire.LocalJWKSURL+`"}}`)

		_, err := f.setup.enableSpire(f.deps, secretsFor())
		require.ErrorContains(t, err, "--spire-datastore-file")
	})

	t.Run("single SPIRE server cluster, datastore given", func(t *testing.T) {
		f := newEnableFixture(t, "10.0.0.2:8300", `{"Config":{"JWKSURL":"https://other.example.com:8082/keys"}}`)
		f.setup.spireDSN = enableTestDSN

		_, err := f.setup.enableSpire(f.deps, secretsFor())
		require.ErrorContains(t, err, "single SPIRE server")
	})

	t.Run("not a server", func(t *testing.T) {
		f := newEnableFixture(t, "10.0.0.2:8300", "")
		require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig, []byte(`{"server":false}`), 0600))

		_, err := f.setup.enableSpire(f.deps, secretsFor())
		require.ErrorContains(t, err, "not a Consul server")
	})

	t.Run("not root", func(t *testing.T) {
		f := newEnableFixture(t, "10.0.0.2:8300", "")
		f.deps.ExpectedCalls = filterCalls(f.deps.ExpectedCalls, "GetuidSyscall")
		f.deps.On("GetuidSyscall").Return(1000)

		_, err := f.setup.enableSpire(f.deps, secretsFor())
		require.ErrorContains(t, err, "root")
	})
}

func TestSetup_enableSpire_singleSpireServer(t *testing.T) {
	t.Run("first server enabled: runs SPIRE, reached through its IP", func(t *testing.T) {
		f := newEnableFixture(t, "10.0.0.2:8300", "")

		_, err := f.setup.enableSpire(f.deps, secretsFor())
		require.NoError(t, err)

		serverConf, err := os.ReadFile(filepath.Join(f.spireDir, spire.ServerConfFile))
		require.NoError(t, err)
		assert.Contains(t, string(serverConf), "sqlite3")

		static := readMain(t, f)["auto_config"].(map[string]any)["authorization"].(map[string]any)["static"].(map[string]any)
		assert.Equal(t, "https://10.0.0.2:8082/keys", static["jwks_url"],
			"the CA of clusters set up before SPIRE is not valid for the hostname")
		require.Len(t, *f.created, 1)
		assert.Contains(t, (*f.created)[0], "https://10.0.0.2:8082/keys")
	})

	t.Run("another server: no SPIRE, uses the one of the first", func(t *testing.T) {
		f := newEnableFixture(t, "10.0.0.2:8300", `{"Config":{"JWKSURL":"https://10.0.0.1:8082/keys"}}`)

		_, err := f.setup.enableSpire(f.deps, secretsFor())
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(f.spireDir, spire.ServerConfFile))
		require.Error(t, err, "SPIRE does not run here")

		static := readMain(t, f)["auto_config"].(map[string]any)["authorization"].(map[string]any)["static"].(map[string]any)
		assert.Equal(t, "https://10.0.0.1:8082/keys", static["jwks_url"])
		assert.Empty(t, *f.created)
		assert.Equal(t, 1, *f.restarts)
	})
}

func filterCalls(calls []*mock.Call, method string) []*mock.Call {
	kept := calls[:0:0]

	for _, call := range calls {
		if call.Method != method {
			kept = append(kept, call)
		}
	}

	return kept
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	return content
}

func TestSetup_readClusterSecrets(t *testing.T) {
	setup := Setup{ClusterCredential: filepath.Join(t.TempDir(), "cred.tar.gpg")}

	out, err := os.Create(setup.ClusterCredential)
	require.NoError(t, err)

	writer, err := encrypter.NewWriter(out, []byte("pw"))
	require.NoError(t, err)

	caPath := strings.TrimPrefix(filepath.Join(config.ConsulHome, command.ConsulCA), "/")
	keyPath := strings.TrimPrefix(filepath.Join(config.ConsulHome, command.ConsulCAKey), "/")

	for name, content := range map[string]string{
		caPath: "CA", keyPath: "KEY", command.ConsulACLBootstrap: `{"SecretID":"bootstrap"}`,
	} {
		buf, info := test.CreateDumbFile([]byte(content), name)
		require.NoError(t, writer.AddFile(buf, info, name, "/"))
	}

	require.NoError(t, writer.Flush())
	writer.Close()
	out.Close()

	secrets, err := setup.readClusterSecrets("pw")
	require.NoError(t, err)
	assert.Equal(t, "bootstrap", secrets.token)
	assert.Equal(t, "KEY", string(secrets.caKeyPEM))

	t.Setenv(command.SetupConsulToken, "from-env")

	secrets, err = setup.readClusterSecrets("pw")
	require.NoError(t, err)
	assert.Equal(t, "from-env", secrets.token)

	_, err = setup.readClusterSecrets("wrong")
	require.Error(t, err)
}

func credentialsFrom(t *testing.T, f *enableFixture) map[string]string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ldap-copy")
	require.NoError(t, os.WriteFile(path, *f.ldapCopy, 0600))

	entries, err := readCredentialEntries(path, "pw")
	require.NoError(t, err)

	out := map[string]string{}
	for _, entry := range entries {
		out[entry.name] = string(entry.content)
	}

	return out
}

// Without high availability the keys are fetched from the first server by every Consul server, whoever is the Raft
// leader: no server but the one running SPIRE may be pointed to its loopback address.
func TestSetup_enableSpire_singleSpireServerNeverUsesTheLoopback(t *testing.T) {
	first := newEnableFixture(t, "10.0.0.2:8300", "")

	_, err := first.setup.enableSpire(first.deps, secretsFor())
	require.NoError(t, err)

	url := "https://10.0.0.2:8082/keys"
	assert.Equal(t, url, credentialsFrom(t, first)[spire.ClusterJWKSURLFile], "new servers get it from the credentials")

	// the second server, whose Raft leader is the first one
	second := newEnableFixture(t, "10.0.0.2:8300", `{"Config":{"JWKSURL":"`+url+`"}}`)

	_, err = second.setup.enableSpire(second.deps, secretsFor())
	require.NoError(t, err)

	for _, f := range []*enableFixture{first, second} {
		static := readMain(t, f)["auto_config"].(map[string]any)["authorization"].(map[string]any)["static"].(map[string]any)
		assert.Equal(t, url, static["jwks_url"])
	}

	for _, call := range append(first.deps.Calls, second.deps.Calls...) {
		for _, arg := range call.Arguments {
			assert.NotContains(t, strings.Join(argStrings(arg), " "), "127.0.0.1:8082")
		}
	}

	assert.Empty(t, *second.created, "the auth method is already right")
}

func argStrings(arg any) []string {
	if text, ok := arg.(string); ok {
		return []string{text}
	}

	return nil
}
