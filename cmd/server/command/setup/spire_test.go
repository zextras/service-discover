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

	cfg, err := setup.generateCertificateAndConfig(mockDep, "mail.example.com", "gossipKey", "https://mail.example.com:8082")
	require.NoError(t, err)

	bs, err := json.Marshal(cfg)
	require.NoError(t, err)

	got := map[string]any{}
	require.NoError(t, json.Unmarshal(bs, &got))
	assert.Equal(t, map[string]any{
		"authorization": map[string]any{
			"enabled": true,
			"static": map[string]any{
				"oidc_discovery_url":     "https://mail.example.com:8082",
				"oidc_discovery_ca_cert": caPEM,
				"bound_issuer":           "https://mail.example.com:8082",
				"bound_audiences":        []any{"consul-auto-config"},
				"claim_mappings":         map[string]any{"sub": "spiffe_id"},
				"claim_assertions":       []any{`value.spiffe_id == "spiffe://carbonio/consul/${node}"`},
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

func TestSetup_clusterOIDCURL(t *testing.T) {
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

	createTarball(map[string]string{"gossip-key": "k", spire.ClusterOIDCURLFile: "https://mail.example.com:8082\n"})

	got, err := setup.clusterOIDCURL()
	require.NoError(t, err)
	assert.Equal(t, "https://mail.example.com:8082", got)

	createTarball(map[string]string{"gossip-key": "k"})

	got, err = setup.clusterOIDCURL()
	require.NoError(t, err)
	assert.Empty(t, got, "tarballs created before SPIRE do not have the OIDC URL")
}

func TestSetup_setupSpireServer(t *testing.T) {
	previous := testingMode
	testingMode = true

	defer func() { testingMode = previous }()

	var oidcChecks []string

	waitOIDCDiscovery = func(url string, caPEM []byte, _, _ time.Duration) error {
		oidcChecks = append(oidcChecks, url+"|"+string(caPEM))

		return nil
	}

	defer func() { waitOIDCDiscovery = spire.WaitOIDCDiscovery }()

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

	oidcURL, err := setup.setupSpireServer(deps, "mail.example.com", "10.0.0.1/24")
	require.NoError(t, err)
	assert.Equal(t, "https://mail.example.com:8082", oidcURL)
	assert.Equal(t, []string{"https://mail.example.com:8082|" + caPEM}, oidcChecks)

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

	oidcConf, err := os.ReadFile(filepath.Join(spireDir, spire.OIDCConfFile))
	require.NoError(t, err)
	assert.Contains(t, string(oidcConf), filepath.Join(spireDir, spire.OIDCCertFile))

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
