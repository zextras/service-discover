// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/legacytoken"
	"github.com/zextras/service-discover/pkg/mesh"
)

const (
	covHost     = "mail.example.com"
	covTokensAt = "/v1/acl/tokens"
	covLegacyID = "legacy-id"
)

var errCov = errors.New("cov failure")

// covConsul plays the token endpoints of Consul: a legacy token of carbonio-files of covHost, and the login token of
// the service that replaces it.
type covConsul struct {
	deleted []string
	secrets []string
	failing bool
}

func (c *covConsul) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.secrets = append(c.secrets, r.Header.Get("X-Consul-Token"))

	switch {
	case c.failing:
		http.Error(w, "boom", http.StatusInternalServerError)
	case r.URL.Path == covTokensAt:
		_, _ = w.Write([]byte(`[
			{"AccessorID":"` + covLegacyID + `","Description":"Token for carbonio-files/` + covHost + `",
			 "Policies":[{"Name":"carbonio-files-policy"}]},
			{"AccessorID":"login-id","AuthMethod":"spire","Roles":[{"Name":"carbonio-files-role"}]}
		]`))
	case r.Method == http.MethodDelete:
		c.deleted = append(c.deleted, strings.TrimPrefix(r.URL.Path, "/v1/acl/token/"))
		_, _ = w.Write([]byte(`true`))
	default:
		http.NotFound(w, r)
	}
}

func startCovConsul(t *testing.T) *covConsul {
	t.Helper()

	fake := &covConsul{}
	server := httptest.NewServer(fake)

	t.Cleanup(server.Close)
	t.Setenv("CONSUL_HTTP_ADDR", strings.TrimPrefix(server.URL, "http://"))
	t.Setenv("CONSUL_HTTP_SSL", "false")

	return fake
}

func TestRevokeWithToken_deletesTheLegacyTokenOfTheHost(t *testing.T) {
	fake := startCovConsul(t)
	selector := legacytoken.NewSelector(command.Server, covHost, func(string) (bool, error) { return true, nil })

	revoked, kept, err := revokeWithToken("the-token", selector)

	require.NoError(t, err)
	assert.Empty(t, kept)
	require.Len(t, revoked, 1)
	assert.Equal(t, "carbonio-files", revoked[0].Service)
	assert.Equal(t, []string{covLegacyID}, fake.deleted)
	assert.Equal(t, "the-token", fake.secrets[0], "the requests carry the bootstrap token")
}

func TestRevokeWithToken_reportsTheFailureOfTheCluster(t *testing.T) {
	fake := startCovConsul(t)
	fake.failing = true

	_, _, err := revokeWithToken("the-token", legacytoken.NewSelector(command.Server, covHost, nil))

	require.ErrorContains(t, err, "unable to list the ACL tokens")
	assert.Empty(t, fake.deleted)
}

func TestRunsSpireServer(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "server.conf")

	previous := spireServerConf
	t.Cleanup(func() { spireServerConf = previous })

	spireServerConf = conf
	assert.False(t, runsSpireServer(), "no configuration of the server")

	require.NoError(t, os.WriteFile(conf, []byte("server {}"), 0o600))
	assert.True(t, runsSpireServer())

	spireServerConf = dir
	assert.False(t, runsSpireServer(), "a directory is not the configuration")
}

func TestServerHostname(t *testing.T) {
	path := filepath.Join(t.TempDir(), "localconfig.xml")

	previous := localConfigPath
	t.Cleanup(func() { localConfigPath = previous })

	localConfigPath = path
	_, err := serverHostname()
	require.Error(t, err, "no localconfig")

	require.NoError(t, os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<localconfig>
  <key name="ldap_master_url"><value>ldap://`+covHost+`:389</value></key>
  <key name="ldap_url"><value>ldap://`+covHost+`:389</value></key>
  <key name="zimbra_ldap_userdn"><value>uid=zimbra,cn=admins,cn=zimbra</value></key>
  <key name="zimbra_ldap_password"><value>password</value></key>
  <key name="zimbra_server_hostname"><value>`+covHost+`</value></key>
</localconfig>`), 0o600))

	hostname, err := serverHostname()

	require.NoError(t, err)
	assert.Equal(t, covHost, hostname)
}

func TestLoginTokens_aServiceWithoutTokenFileHasNoLogin(t *testing.T) {
	startCovConsul(t)

	hasLogin, err := loginTokens("carbonio")

	require.NoError(t, err)
	assert.False(t, hasLogin(&mesh.Definition{Name: "cov-not-installed", User: "cov-not-installed"}))
}

func TestNew_wiresTheCommandToTheMachine(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	bootstrap := &command.BootstrapToken{}

	apply := New(bootstrap, out).Apply

	assert.Same(t, bootstrap, apply.bootstrap)
	assert.Same(t, out, apply.writer)
	assert.Equal(t, os.Getuid(), apply.getuid())
	assert.NotNil(t, apply.apply)
	assert.NotNil(t, apply.runsSpire)
	assert.NotNil(t, apply.hostname)
	assert.NotNil(t, apply.entries)
	assert.NotNil(t, apply.hasEntry)
	assert.NotNil(t, apply.loginInPlace)
	assert.NotNil(t, apply.revoke)

	defs, err := apply.definitions()
	require.NoError(t, err)
	assert.NotEmpty(t, defs, "the definitions are the ones of the specs directory")
}

func TestApply_failures(t *testing.T) {
	failHostname := func(a *Apply) { a.hostname = func() (string, error) { return "", errCov } }
	failDefinitions := func(a *Apply) { a.definitions = func() ([]mesh.Definition, error) { return nil, errCov } }

	tests := []struct {
		name   string
		revoke bool
		tweak  func(a *Apply)
	}{
		{name: "hostname of the SPIRE server", tweak: failHostname},
		{name: "definitions of the SPIRE server", tweak: failDefinitions},
		{name: "entries of the SPIRE server", tweak: func(a *Apply) { a.entries = failingEnsurer{} }},
		{name: "hostname for the revocation", revoke: true, tweak: func(a *Apply) {
			a.runsSpire = func() bool { return false }
			failHostname(a)
		}},
		{name: "definitions for the revocation", revoke: true, tweak: func(a *Apply) {
			a.runsSpire = func() bool { return false }
			failDefinitions(a)
		}},
		{name: "login tokens of the node", revoke: true, tweak: func(a *Apply) {
			a.loginInPlace = func(string) (func(*mesh.Definition) bool, error) { return nil, errCov }
		}},
		{name: "SPIRE entry lookup of the revocation", revoke: true, tweak: func(a *Apply) {
			a.revoke = (&revoker{}).revoke
			a.hasEntry = func(string, string) (bool, error) { return false, errCov }
			a.loginInPlace = func(string) (func(*mesh.Definition) bool, error) {
				return func(*mesh.Definition) bool { return true }, nil
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fix := newFixture()
			cmd := fix.command(true, rootUID)
			cmd.bootstrap = &command.BootstrapToken{}
			cmd.RevokeLegacy = tt.revoke
			tt.tweak(cmd)

			require.ErrorIs(t, run(t, cmd), errCov)
		})
	}
}

type failingEnsurer struct{}

func (failingEnsurer) EnsureEntry(string, string, ...string) (bool, error) { return false, errCov }

func TestApply_theBootstrapTokenIsRequired(t *testing.T) {
	t.Setenv(command.SetupConsulToken, "")

	fix := newFixture()
	cmd := fix.command(true, rootUID)
	cmd.bootstrap = &command.BootstrapToken{Password: "p"}

	require.Error(t, cmd.Run(&command.GlobalCommonFlags{}), "the credentials of the cluster are not there")
	assert.Empty(t, fix.applied)
}
