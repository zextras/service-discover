// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command/setup/mocks"
	"github.com/zextras/service-discover/pkg/encrypter"
	mocks22 "github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/spire"
	mocks3 "github.com/zextras/service-discover/pkg/systemd/mocks"
	"github.com/zextras/service-discover/test"
)

func writeTestCA(t *testing.T, dir string) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:                big.NewInt(1),
		Subject:                     pkix.Name{CommonName: "Consul Agent CA"},
		NotBefore:                   time.Now().Add(-time.Hour),
		NotAfter:                    time.Now().Add(time.Hour),
		IsCA:                        true,
		BasicConstraintsValid:       true,
		KeyUsage:                    x509.KeyUsageCertSign,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{"mail.example.com", "consul", "localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "consul-agent-ca.pem"), caPEM, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "consul-agent-ca-key.pem"),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600))

	return string(caPEM)
}

func mockOwnership(deps *mocks.BusinessDependencies) {
	deps.On("LookupUser", mock.Anything).Return(&user.User{Uid: "10", Gid: "20"}, nil).
		On("LookupGroup", mock.Anything).Return(&user.Group{Gid: "20"}, nil).
		On("Chown", mock.AnythingOfType("string"), 10, 20).Return(nil).
		On("Chmod", mock.AnythingOfType("string"), mock.Anything).Return(nil)
}

func TestSetup_generateCertificateAndConfig_autoConfig(t *testing.T) {
	consulHome := t.TempDir()
	caPEM := writeTestCA(t, consulHome)
	setup := Setup{ConsulHome: consulHome}

	mockDep := new(mocks.BusinessDependencies)
	tlsCommand := new(mocks22.Cmd)
	tlsCommand.On("Output").Return([]uint8{}, nil)
	mockDep.On("CreateCommand", "/usr/bin/consul", "tls", "cert", "create", "-days=10950", "-server").
		Return(tlsCommand)
	mockDep.On("LookupUser", "service-discover").Return(&user.User{Uid: "1234", Gid: "0"}, nil).
		On("LookupGroup", "service-discover").Return(&user.Group{Gid: "123456"}, nil).
		On("Chown", mock.AnythingOfType("string"), 1234, 123456).Return(nil).
		On("Chmod", mock.AnythingOfType("string"), os.FileMode(0600)).Return(nil)

	cfg, err := setup.generateCertificateAndConfig(mockDep, "mail.example.com", "gossipKey", "https://mail.example.com:8082/keys")
	require.NoError(t, err)

	bs, err := json.Marshal(cfg)
	require.NoError(t, err)

	got := map[string]any{}
	require.NoError(t, json.Unmarshal(bs, &got))
	assert.Equal(t, map[string]any{
		"authorization": map[string]any{
			"enabled": true,
			"static": map[string]any{
				"jwks_url":         "https://mail.example.com:8082/keys",
				"jwks_ca_cert":     caPEM,
				"bound_issuer":     "spiffe://carbonio",
				"bound_audiences":  []any{"consul-auto-config"},
				"claim_mappings":   map[string]any{"sub": "spiffe_id"},
				"claim_assertions": []any{`value.spiffe_id == "spiffe://carbonio/consul/${node}"`},
			},
		},
	}, got["auto_config"])
}

func TestSetup_generateCertificateAndConfig_withoutOIDC(t *testing.T) {
	cfg := &setupConfig{}
	bs, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(bs), "auto_config", "servers of clusters without SPIRE must keep the old config")
}

func TestSetup_clusterJWKSURL(t *testing.T) {
	setup := Setup{ClusterCredential: filepath.Join(t.TempDir(), "cred.tar.gpg"), Password: "pw"}

	createTarball := func(files map[string]string) {
		out, err := os.Create(setup.ClusterCredential)
		require.NoError(t, err)

		defer out.Close()

		writer, err := encrypter.NewWriter(out, []byte("pw"))
		require.NoError(t, err)

		for name, content := range files {
			buf, info := test.CreateDumbFile([]byte(content), name)
			require.NoError(t, writer.AddFile(buf, info, name, "/"))
		}

		require.NoError(t, writer.Flush())
		writer.Close()
	}

	createTarball(map[string]string{"gossip-key": "k", spire.ClusterJWKSURLFile: "https://mail.example.com:8082/keys\n"})

	got, err := setup.clusterJWKSURL()
	require.NoError(t, err)
	assert.Equal(t, "https://mail.example.com:8082/keys", got)

	createTarball(map[string]string{"gossip-key": "k"})

	got, err = setup.clusterJWKSURL()
	require.NoError(t, err)
	assert.Empty(t, got, "tarballs created before SPIRE do not have the keys URL")
}

func TestSetup_setupSpireServer(t *testing.T) {
	previous := testingMode
	testingMode = true

	defer func() { testingMode = previous }()

	var oidcChecks []string

	waitJWKS = func(url string, caPEM []byte, _, _ time.Duration) error {
		oidcChecks = append(oidcChecks, url+"|"+string(caPEM))

		return nil
	}

	defer func() { waitJWKS = spire.WaitJWKS }()

	consulHome, spireDir := t.TempDir(), filepath.Join(t.TempDir(), "spire")
	caPEM := writeTestCA(t, consulHome)
	setup := Setup{ConsulHome: consulHome, SpireConfigDir: spireDir}

	health := new(mocks22.Cmd)
	health.On("Output").Return([]byte("healthy"), nil)

	unit := new(mocks3.UnitManager)
	unit.On("StartUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	unit.On("EnableUnitFiles", mock.Anything, false, false).Return(false, nil, nil)
	unit.On("Close").Return()

	deps := new(mocks.BusinessDependencies)
	mockOwnership(deps)
	deps.On("SystemdUnitHandler").Return(unit, nil).
		On("CreateCommand", spire.ServerBin, "healthcheck", "-socketPath", spire.ServerSocket).Return(health)

	jwksURL, err := setup.setupSpireServer(deps, "mail.example.com", "10.0.0.1/24", false)
	require.NoError(t, err)
	assert.Equal(t, "https://mail.example.com:8082/keys", jwksURL)
	assert.Equal(t, []string{"https://mail.example.com:8082/keys|" + caPEM}, oidcChecks)

	for name, mode := range map[string]os.FileMode{
		spire.ServerConfFile: 0640, spire.OIDCConfFile: 0640, spire.OIDCCertFile: 0640, spire.OIDCKeyFile: 0600,
	} {
		info, err := os.Stat(filepath.Join(spireDir, name))
		require.NoError(t, err, name)
		assert.Equal(t, mode, info.Mode().Perm(), name)
	}

	serverConf, err := os.ReadFile(filepath.Join(spireDir, spire.ServerConfFile))
	require.NoError(t, err)
	assert.Contains(t, string(serverConf), `bind_address = "10.0.0.1"`)
	assert.Contains(t, string(serverConf), `sqlite3`)
	assert.Contains(t, string(serverConf), `jwt_issuer = "spiffe://carbonio"`)

	oidcConf, err := os.ReadFile(filepath.Join(spireDir, spire.OIDCConfFile))
	require.NoError(t, err)
	assert.Contains(t, string(oidcConf), filepath.Join(spireDir, spire.OIDCCertFile))
	assert.Contains(t, string(oidcConf), `addr = "10.0.0.1:8082"`, "other servers reach the only OIDC provider")

	for _, name := range []string{spire.ServerUnit, spire.OIDCUnit} {
		unit.AssertCalled(t, "StartUnit", name, "replace", mock.Anything)
		unit.AssertCalled(t, "EnableUnitFiles", []string{name}, false, false)
	}

	unit.AssertNotCalled(t, "StartUnit", spire.AgentUnit, "replace", mock.Anything)
}

func TestSetup_setupSpireAgent(t *testing.T) {
	previous := testingMode
	testingMode = true

	defer func() { testingMode = previous }()

	spireDir := filepath.Join(t.TempDir(), "spire")
	setup := Setup{SpireConfigDir: spireDir}

	output := func(out string) *mocks22.Cmd {
		cmd := new(mocks22.Cmd)
		cmd.On("Output").Return([]byte(out), nil)

		return cmd
	}

	unit := new(mocks3.UnitManager)
	unit.On("StartUnit", spire.AgentUnit, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	unit.On("EnableUnitFiles", []string{spire.AgentUnit}, false, false).Return(false, nil, nil)
	unit.On("Close").Return()

	deps := new(mocks.BusinessDependencies)
	mockOwnership(deps)
	deps.On("SystemdUnitHandler").Return(unit, nil).
		On("CreateCommand", spire.ServerBin, "healthcheck", "-socketPath", spire.ServerSocket).
		Return(output("healthy")).
		On("CreateCommand", spire.ServerBin, "bundle", "show", "-socketPath", spire.ServerSocket).
		Return(output("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")).
		On("CreateCommand", spire.ServerBin, "token", "generate", "-spiffeID", "spiffe://carbonio/node/mail.example.com",
			"-ttl", "600", "-output", "json", "-socketPath", spire.ServerSocket).
		Return(output(`{"value":"join-me"}`))

	require.NoError(t, setup.setupSpireAgent(deps, "mail.example.com", "10.0.0.1"))

	agentConf, err := os.ReadFile(filepath.Join(spireDir, spire.AgentConfFile))
	require.NoError(t, err)
	assert.Contains(t, string(agentConf), `join_token = "join-me"`)
	assert.Contains(t, string(agentConf), `server_address = "10.0.0.1"`)

	info, err := os.Stat(filepath.Join(spireDir, spire.AgentConfFile))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "agent.conf contains the join token")

	bundle, err := os.ReadFile(filepath.Join(spireDir, spire.BundleFile))
	require.NoError(t, err)
	assert.Contains(t, string(bundle), "AAAA")

	unit.AssertCalled(t, "StartUnit", spire.AgentUnit, "replace", mock.Anything)
}

func TestSetup_setupSpireServer_highAvailability(t *testing.T) {
	previous := testingMode
	testingMode = true

	defer func() { testingMode = previous }()

	var checked []string

	waitJWKS = func(url string, _ []byte, _, _ time.Duration) error {
		checked = append(checked, url)

		return nil
	}

	defer func() { waitJWKS = spire.WaitJWKS }()

	unit := new(mocks3.UnitManager)
	unit.On("StartUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	unit.On("EnableUnitFiles", mock.Anything, false, false).Return(false, nil, nil)
	unit.On("Close").Return()

	health := new(mocks22.Cmd)
	health.On("Output").Return([]byte("healthy"), nil)

	const dsn = "postgres://spire:pw@db.example.com:5432/spire?sslmode=verify-full"

	// The first server and the additional ones: the latter cannot get a certificate for their own hostname, because
	// the Consul CA is name constrained to the hostname of the first one.
	for name, additional := range map[string]bool{"first": false, "additional": true} {
		consulHome, spireDir := t.TempDir(), filepath.Join(t.TempDir(), "spire")
		writeTestCA(t, consulHome)
		setup := Setup{ConsulHome: consulHome, SpireConfigDir: spireDir, spireDSN: dsn}

		deps := new(mocks.BusinessDependencies)
		mockOwnership(deps)
		deps.On("SystemdUnitHandler").Return(unit, nil).
			On("CreateCommand", spire.ServerBin, "healthcheck", "-socketPath", spire.ServerSocket).Return(health)

		hostname := "mail.example.com"
		if additional {
			hostname = "mail2.example.com"
		}

		jwksURL, err := setup.setupSpireServer(deps, hostname, "10.0.0.2", additional)
		require.NoError(t, err, name)
		assert.Equal(t, "https://127.0.0.1:8082/keys", jwksURL, name)
		assert.Equal(t, spire.LocalJWKSURL, checked[len(checked)-1], name)

		serverConf, err := os.ReadFile(filepath.Join(spireDir, spire.ServerConfFile))
		require.NoError(t, err)
		assert.Contains(t, string(serverConf), `database_type = "postgres"`, name)
		assert.Contains(t, string(serverConf), dsn, name)

		info, err := os.Stat(filepath.Join(spireDir, spire.ServerConfFile))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "server.conf holds the database credentials")

		oidcConf, err := os.ReadFile(filepath.Join(spireDir, spire.OIDCConfFile))
		require.NoError(t, err)
		assert.Contains(t, string(oidcConf), `addr = "127.0.0.1:8082"`, name)

		certPEM, err := os.ReadFile(filepath.Join(spireDir, spire.OIDCCertFile))
		require.NoError(t, err)

		block, _ := pem.Decode(certPEM)
		leaf, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)

		pool := x509.NewCertPool()
		caPEM, err := os.ReadFile(filepath.Join(consulHome, "consul-agent-ca.pem"))
		require.NoError(t, err)
		require.True(t, pool.AppendCertsFromPEM(caPEM))

		_, err = leaf.Verify(x509.VerifyOptions{DNSName: "127.0.0.1", Roots: pool})
		require.NoError(t, err, "%s: the local Consul validates the JWT-SVIDs through the loopback address", name)
	}
}

func TestSetup_importSpireMode(t *testing.T) {
	t.Parallel()

	ha, single := Setup{spireDSN: enableTestDSN}, Setup{}

	run, err := ha.importSpireMode(spire.LocalJWKSURL)
	require.NoError(t, err)
	assert.True(t, run, "every server of a high availability cluster runs SPIRE")

	_, err = single.importSpireMode(spire.LocalJWKSURL)
	require.ErrorContains(t, err, "--spire-datastore-file")

	run, err = single.importSpireMode("https://mail.example.com:8082/keys")
	require.NoError(t, err)
	assert.False(t, run, "the additional server of a cluster with a single SPIRE server does not run it")

	_, err = ha.importSpireMode("https://mail.example.com:8082/keys")
	require.Error(t, err)

	run, err = single.importSpireMode("")
	require.NoError(t, err)
	assert.False(t, run, "clusters set up before SPIRE keep the old behaviour")

	_, err = ha.importSpireMode("")
	require.Error(t, err)
}
