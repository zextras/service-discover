// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pkgexec "github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/spire"
)

func validBundle() *spire.JoinBundle {
	return &spire.JoinBundle{
		Version:       spire.BundleVersion,
		TrustDomain:   spire.TrustDomain,
		SpireServer:   "mail.example.com:8081",
		SpireBundle:   "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n",
		JoinToken:     "6d645c6a-7f50-47fd-8e69-17b8fd7426ca",
		ConsulCA:      "-----BEGIN CERTIFICATE-----\nBBBB\n-----END CERTIFICATE-----\n",
		ConsulServers: []string{"10.0.0.1:8300", "10.0.0.2:8300"},
		NodeName:      "agent-mail2-example-com",
	}
}

func TestJoinBundle_roundTrip(t *testing.T) {
	t.Parallel()

	want := validBundle()

	encoded, err := want.Encode()
	require.NoError(t, err)
	assert.NotContains(t, encoded, "=", "base64url without padding")
	assert.NotContains(t, encoded, "+")
	assert.NotContains(t, encoded, "/")

	got, err := spire.DecodeJoinBundle("  " + encoded + "\n")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestJoinBundle_decodeErrors(t *testing.T) {
	t.Parallel()

	noToken := validBundle()
	noToken.JoinToken = ""
	_, err := noToken.Encode()
	require.Error(t, err)

	wrongDomain := validBundle()
	wrongDomain.TrustDomain = "other"
	_, err = wrongDomain.Encode()
	require.Error(t, err)

	wrongVersion := validBundle()
	wrongVersion.Version = 2
	_, err = wrongVersion.Encode()
	require.Error(t, err)

	_, err = spire.DecodeJoinBundle("not base64url!!")
	require.Error(t, err)

	_, err = spire.DecodeJoinBundle(base64.RawURLEncoding.EncodeToString([]byte("not json")))
	require.Error(t, err)

	_, err = spire.DecodeJoinBundle(base64.RawURLEncoding.EncodeToString([]byte(`{"version":1}`)))
	require.Error(t, err)
}

func TestIdentities(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "spiffe://carbonio/node/mail.example.com", spire.NodeID("mail.example.com"))
	assert.Equal(t, "spiffe://carbonio/consul/agent-mail-example-com", spire.ConsulID("agent-mail-example-com"))
	assert.Equal(t, "spiffe://carbonio/service/carbonio-files", spire.ServiceID("carbonio-files"))
	assert.Equal(t, "https://mail.example.com:8082", spire.OIDCURL("mail.example.com"))
}

func TestConfigs(t *testing.T) {
	t.Parallel()

	server := spire.ServerConf("10.0.0.1", "mail.example.com")
	for _, want := range []string{
		`bind_address = "10.0.0.1"`,
		`bind_port = "8081"`,
		`socket_path = "/run/spire/server/api.sock"`,
		`trust_domain = "carbonio"`,
		`data_dir = "/var/lib/spire/server"`,
		`jwt_issuer = "https://mail.example.com:8082"`,
		`default_jwt_svid_ttl = "5m"`,
		`NodeAttestor "join_token"`,
		`KeyManager "disk"`,
		`database_type = "sqlite3"`,
	} {
		assert.Contains(t, server, want)
	}

	oidc := spire.OIDCConf("/etc/zextras/spire", "10.0.0.1", "mail.example.com")
	for _, want := range []string{
		`domains = ["mail.example.com", "10.0.0.1"]`,
		`serving_cert_source "cert_file"`,
		`cert_file_path = "/etc/zextras/spire/oidc-cert.pem"`,
		`key_file_path = "/etc/zextras/spire/oidc-key.pem"`,
		`addr = "10.0.0.1:8082"`,
		`address = "unix:///run/spire/server/api.sock"`,
	} {
		assert.Contains(t, oidc, want)
	}

	agent := spire.AgentConf("/etc/zextras/spire", "mail.example.com", "tok")
	for _, want := range []string{
		`server_address = "mail.example.com"`,
		`server_port = "8081"`,
		`socket_path = "/run/spire/agent/api.sock"`,
		`trust_bundle_path = "/etc/zextras/spire/bundle.pem"`,
		`join_token = "tok"`,
		`data_dir = "/var/lib/spire/agent"`,
		`WorkloadAttestor "unix"`,
		`KeyManager "disk"`,
	} {
		assert.Contains(t, agent, want)
	}
	assert.NotContains(t, agent, "insecure_bootstrap")
}

func newCA(t *testing.T) ([]byte, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Consul Agent CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		// Like the CA created by "consul tls ca create -name-constraint".
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{"mail.example.com", "consul", "localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func TestSignServingCert(t *testing.T) {
	t.Parallel()

	caPEM, caKeyPEM := newCA(t)
	ip := net.ParseIP("10.0.0.1")

	certPEM, keyPEM, err := spire.SignServingCert(caPEM, caKeyPEM, "mail.example.com", ip, time.Hour)
	require.NoError(t, err)

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err, "key must match the certificate")

	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	require.NoError(t, err)

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))

	for _, name := range []string{"mail.example.com"} {
		_, err = leaf.Verify(x509.VerifyOptions{DNSName: name, Roots: pool})
		require.NoError(t, err)
	}

	_, err = leaf.Verify(x509.VerifyOptions{DNSName: "10.0.0.1", Roots: pool})
	require.NoError(t, err)

	_, err = leaf.Verify(x509.VerifyOptions{DNSName: "other.example.com", Roots: pool})
	require.Error(t, err)

	_, _, err = spire.SignServingCert([]byte("junk"), caKeyPEM, "mail.example.com", ip, time.Hour)
	require.Error(t, err)

	_, _, err = spire.SignServingCert(caPEM, []byte("junk"), "mail.example.com", ip, time.Hour)
	require.Error(t, err)
}

func TestWaitOIDCDiscovery(t *testing.T) {
	t.Parallel()

	caPEM, caKeyPEM := newCA(t)
	certPEM, keyPEM, err := spire.SignServingCert(caPEM, caKeyPEM, "localhost", net.ParseIP("127.0.0.1"), time.Hour)
	require.NoError(t, err)

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)

	ready := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-ready:
			if r.URL.Path == "/.well-known/openid-configuration" {
				_, _ = w.Write([]byte("{}"))

				return
			}
		default:
		}

		http.NotFound(w, r)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	server.StartTLS()

	defer server.Close()

	url := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)

	err = spire.WaitOIDCDiscovery(url, caPEM, 200*time.Millisecond, 20*time.Millisecond)
	require.Error(t, err, "not ready yet")

	go func() {
		time.Sleep(100 * time.Millisecond)
		close(ready)
	}()

	require.NoError(t, spire.WaitOIDCDiscovery(url, caPEM, 5*time.Second, 20*time.Millisecond))

	otherCA, _ := newCA(t)
	require.Error(t, spire.WaitOIDCDiscovery(url, otherCA, 100*time.Millisecond, 20*time.Millisecond),
		"a server not signed by the given CA must not be trusted")
	require.Error(t, spire.WaitOIDCDiscovery(url, []byte("junk"), time.Second, 20*time.Millisecond))
}

// cmdFor returns a command factory answering every spire-server invocation with the response registered for the
// subcommand (first argument), and recording the arguments.
func cmdFor(t *testing.T, responses map[string]string, calls *[][]string) func(string, ...string) pkgexec.Cmd {
	t.Helper()

	return func(name string, args ...string) pkgexec.Cmd {
		assert.Equal(t, spire.ServerBin, name)

		*calls = append(*calls, args)

		cmd := new(mocks.Cmd)
		out, ok := responses[strings.Join(args[:2], " ")]
		if !ok {
			out, ok = responses[args[0]]
		}

		if !ok {
			cmd.On("Output").Return(nil, &exec.ExitError{Stderr: []byte("unexpected command")})
		} else {
			cmd.On("Output").Return([]byte(out), nil)
		}

		return cmd
	}
}

func TestServerCLI(t *testing.T) {
	t.Parallel()

	var calls [][]string

	cli := spire.NewServerCLI(cmdFor(t, map[string]string{
		"token generate": `{"expires_at":"1","value":"the-token"}`,
		"entry show":     `{"entries":[],"next_page_token":""}`,
		"entry create":   "Entry ID         : 1",
		"bundle":         "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n\n",
		"healthcheck":    "Server is healthy.",
	}, &calls))

	require.NoError(t, cli.WaitHealthy(time.Second, time.Millisecond))

	token, err := cli.GenerateJoinToken(spire.NodeID("h"), 90*time.Second+time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, "the-token", token)
	assert.Contains(t, calls, []string{"token", "generate", "-spiffeID", "spiffe://carbonio/node/h", "-ttl", "91",
		"-output", "json", "-socketPath", spire.ServerSocket})

	created, err := cli.EnsureEntry(spire.NodeID("h"), spire.ConsulID("agent-h"), "unix:uid:0")
	require.NoError(t, err)
	assert.True(t, created)
	assert.Contains(t, calls, []string{"entry", "create", "-parentID", "spiffe://carbonio/node/h",
		"-spiffeID", "spiffe://carbonio/consul/agent-h", "-selector", "unix:uid:0", "-socketPath", spire.ServerSocket})

	bundle, err := cli.BundlePEM()
	require.NoError(t, err)
	assert.Equal(t, "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n", bundle)
}

func TestServerCLI_ensureEntryIsIdempotent(t *testing.T) {
	t.Parallel()

	var calls [][]string

	cli := spire.NewServerCLI(cmdFor(t, map[string]string{
		"entry show": `{"entries":[{"id":"x"}],"next_page_token":""}`,
	}, &calls))

	created, err := cli.EnsureEntry(spire.NodeID("h"), spire.ConsulID("agent-h"), "unix:uid:0")
	require.NoError(t, err)
	assert.False(t, created)
	assert.Len(t, calls, 1, "no entry must be created")
}

func TestServerCLI_errors(t *testing.T) {
	t.Parallel()

	var calls [][]string

	cli := spire.NewServerCLI(cmdFor(t, map[string]string{
		"token generate": `{"value":""}`,
	}, &calls))

	_, err := cli.GenerateJoinToken(spire.NodeID("h"), time.Minute)
	require.Error(t, err)

	_, err = cli.BundlePEM()
	require.ErrorContains(t, err, "unexpected command")

	require.Error(t, cli.WaitHealthy(10*time.Millisecond, time.Millisecond))
}

type ownershipDeps struct {
	chowns map[string][2]int
}

func (o *ownershipDeps) LookupUser(name string) (*user.User, error) {
	return &user.User{Uid: "10", Gid: "20", Username: name}, nil
}

func (o *ownershipDeps) LookupGroup(string) (*user.Group, error) { return &user.Group{Gid: "20"}, nil }

func (o *ownershipDeps) Chown(path string, uid, gid int) error {
	o.chowns[path] = [2]int{uid, gid}

	return nil
}

func (o *ownershipDeps) Chmod(path string, mode os.FileMode) error { return os.Chmod(path, mode) }

func TestWriteFile(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "spire")
	deps := &ownershipDeps{chowns: map[string][2]int{}}

	path, err := spire.WriteFile(deps, dir, "agent.conf", []byte("content"), spire.AgentUser, 0600)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "agent.conf"), path)
	assert.Equal(t, [2]int{10, 20}, deps.chowns[path])

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	bs, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "content", string(bs))
}

func TestEnsureAuthMethod(t *testing.T) {
	notFound := &exec.ExitError{Stderr: []byte(`Auth method not found with name "spire"`)}

	run := func(readErr error, wantSecond string) {
		var calls []string

		creator := func(_ string, args ...string) pkgexec.Cmd {
			calls = append(calls, strings.Join(args[:3], " "))

			cmd := new(mocks.Cmd)
			if args[2] == "read" {
				cmd.On("Output").Return(nil, readErr)
			} else {
				assert.Contains(t, args, "-config")
				cmd.On("Output").Return([]byte("ok"), nil)
			}

			return cmd
		}

		require.NoError(t, spire.EnsureAuthMethod(creator, "root-token", "https://h:8082", "CA"))
		assert.Equal(t, []string{"acl auth-method read", wantSecond}, calls)
	}

	run(notFound, "acl auth-method create")
	run(nil, "acl auth-method update")

	failing := func(string, ...string) pkgexec.Cmd {
		cmd := new(mocks.Cmd)
		cmd.On("Output").Return(nil, &exec.ExitError{Stderr: []byte("connection refused")})

		return cmd
	}
	require.ErrorContains(t, spire.EnsureAuthMethod(failing, "t", "u", "ca"), "connection refused")
}
