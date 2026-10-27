// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fakeBootstrapToken = "0f3c1c55-2b8e-4f43-9b1e-6d7f0a2b9c11"
	fakeNodeName       = "server-mail1-example-com"
	fakePolicyID       = "6a1f3e2d-0000-4000-8000-000000000001"
)

// fakeConsul is a minimal in-memory Consul ACL HTTP API.
type fakeConsul struct {
	mu        sync.Mutex
	tokens    map[string]*api.ACLToken
	failReads bool
	noLeader  bool
	creates   int
}

func newFakeConsul(t *testing.T) (*fakeConsul, *httptest.Server) {
	t.Helper()

	f := &fakeConsul{tokens: map[string]*api.ACLToken{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	t.Setenv("CONSUL_HTTP_ADDR", srv.URL)
	t.Setenv(ConsulHTTPToken, fakeBootstrapToken)

	return f, srv
}

func (f *fakeConsul) add(token *api.ACLToken) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tokens[token.AccessorID] = token
}

func (f *fakeConsul) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("X-Consul-Token") != fakeBootstrapToken {
		http.Error(w, "ACL not found", http.StatusForbidden)

		return
	}

	path := r.URL.Path

	switch {
	case r.Method == http.MethodGet && path == "/v1/acl/token/self":
		if f.noLeader {
			http.Error(w, "No cluster leader", http.StatusInternalServerError)

			return
		}

		writeJSON(w, api.ACLToken{AccessorID: "bootstrap", SecretID: fakeBootstrapToken})
	case r.Method == http.MethodGet && path == "/v1/agent/self":
		writeJSON(w, map[string]map[string]any{"Config": {"NodeName": fakeNodeName}})
	case r.Method == http.MethodGet && path == "/v1/acl/policy/name/carbonio-files-policy":
		writeJSON(w, api.ACLPolicy{ID: fakePolicyID, Name: "carbonio-files-policy"})
	case r.Method == http.MethodGet && path == "/v1/acl/tokens":
		var out []*api.ACLTokenListEntry

		for _, t := range f.tokens {
			if r.URL.Query().Get("policy") == fakePolicyID {
				out = append(out, &api.ACLTokenListEntry{AccessorID: t.AccessorID, Description: t.Description})
			}
		}

		writeJSON(w, out)
	case r.Method == http.MethodPut && path == "/v1/acl/token":
		var token api.ACLToken

		_ = json.NewDecoder(r.Body).Decode(&token)
		token.SecretID = uuid.NewString()
		f.tokens[token.AccessorID] = &token
		f.creates++

		writeJSON(w, token)
	case strings.HasPrefix(path, "/v1/acl/token/"):
		f.serveToken(w, r, strings.TrimPrefix(path, "/v1/acl/token/"))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeConsul) serveToken(w http.ResponseWriter, r *http.Request, id string) {
	if f.failReads {
		http.Error(w, "rpc error: no cluster leader", http.StatusInternalServerError)

		return
	}

	token, ok := f.tokens[id]
	if !ok {
		http.Error(w, "token not found", http.StatusNotFound)

		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, token)
	case http.MethodPut:
		_ = json.NewDecoder(r.Body).Decode(token)
		writeJSON(w, token)
	case http.MethodDelete:
		delete(f.tokens, id)
		writeJSON(w, true)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func currentUserName(t *testing.T) string {
	t.Helper()

	u, err := user.Current()
	require.NoError(t, err)

	return u.Username
}

func TestEnsureRunAdoptsSameTokenAfterTokenFileLoss(t *testing.T) {
	fake, _ := newFakeConsul(t)
	tokenFile := filepath.Join(t.TempDir(), "service-discover", "token")
	cmd := &EnsureServiceToken{
		Service:   testService,
		Policy:    testPolicy,
		TokenFile: tokenFile,
		Owner:     currentUserName(t),
	}

	require.NoError(t, cmd.Run(nil))

	first, err := os.ReadFile(tokenFile)
	require.NoError(t, err)

	info, err := os.Stat(tokenFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, os.Remove(tokenFile))
	require.NoError(t, cmd.Run(nil))

	second, err := os.ReadFile(tokenFile)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
	assert.Equal(t, 1, fake.creates)
	assert.Contains(t, fake.tokens, ServiceTokenAccessorID(testService, fakeNodeName))
}

func TestEnsureRunRelinksPolicyOnExistingToken(t *testing.T) {
	fake, _ := newFakeConsul(t)
	id := ServiceTokenAccessorID(testService, fakeNodeName)
	fake.add(&api.ACLToken{AccessorID: id, SecretID: "3c9a7f52-0d5e-4b8e-a7f1-2b6d9e0c4a13"})

	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, (&EnsureServiceToken{Service: testService, Policy: testPolicy, TokenFile: tokenFile}).Run(nil))

	content, err := os.ReadFile(tokenFile)
	require.NoError(t, err)
	assert.Equal(t, "3c9a7f52-0d5e-4b8e-a7f1-2b6d9e0c4a13", string(content))
	assert.Equal(t, testPolicy, fake.tokens[id].Policies[0].Name)
	assert.Zero(t, fake.creates)
}

func TestEnsureRunFailsWithoutCreatingOnConsulError(t *testing.T) {
	fake, _ := newFakeConsul(t)
	fake.failReads = true
	tokenFile := filepath.Join(t.TempDir(), "token")

	err := (&EnsureServiceToken{Service: testService, Policy: testPolicy, TokenFile: tokenFile}).Run(nil)

	require.Error(t, err)
	assert.Zero(t, fake.creates)
	assert.NoFileExists(t, tokenFile)
}

func TestEnsureRunRejectsInvalidBootstrapToken(t *testing.T) {
	fake, _ := newFakeConsul(t)
	t.Setenv(ConsulHTTPToken, "9d2b6c1e-0000-4000-8000-00000000dead")
	tokenFile := filepath.Join(t.TempDir(), "token")

	err := (&EnsureServiceToken{Service: testService, Policy: testPolicy, TokenFile: tokenFile}).Run(nil)

	require.ErrorContains(t, err, "not a valid Consul token")
	assert.Zero(t, fake.creates)
	assert.NoFileExists(t, tokenFile)
}

func TestEnsureRunReportsUnreachableConsul(t *testing.T) {
	_, srv := newFakeConsul(t)
	srv.Close()

	err := (&EnsureServiceToken{
		Service:   testService,
		Policy:    testPolicy,
		TokenFile: filepath.Join(t.TempDir(), "token"),
	}).Run(nil)

	require.ErrorContains(t, err, "unable to reach Consul")
}

func TestEnsureRunFailsForUnknownOwner(t *testing.T) {
	newFakeConsul(t)
	tokenFile := filepath.Join(t.TempDir(), "token")

	err := (&EnsureServiceToken{
		Service:   testService,
		Policy:    testPolicy,
		TokenFile: tokenFile,
		Owner:     "no-such-user-co3612",
	}).Run(nil)

	require.ErrorContains(t, err, "no-such-user-co3612")
	assert.NoFileExists(t, tokenFile)
}

func TestPruneLegacyRunDeletesOnlyThisHostLegacyTokens(t *testing.T) {
	fake, _ := newFakeConsul(t)

	hostname, err := os.Hostname()
	require.NoError(t, err)

	current := ServiceTokenAccessorID(testService, fakeNodeName)
	fake.add(&api.ACLToken{AccessorID: current, Description: ServiceTokenDescription(testService, fakeNodeName)})
	fake.add(&api.ACLToken{AccessorID: "legacy-local", Description: "Token for\n    carbonio-files/" + hostname + " "})
	fake.add(&api.ACLToken{AccessorID: "legacy-alias", Description: "Token for carobonio-files/" + hostname})
	fake.add(&api.ACLToken{AccessorID: "legacy-remote", Description: "Token for carbonio-files/remote-co3612.invalid"})

	cmd := &PruneLegacyServiceTokens{Service: testService, Policy: testPolicy, LegacyService: []string{"carobonio-files"}}
	require.NoError(t, cmd.Run(nil))

	assert.Contains(t, fake.tokens, current)
	assert.Contains(t, fake.tokens, "legacy-remote")
	assert.NotContains(t, fake.tokens, "legacy-local")
	assert.NotContains(t, fake.tokens, "legacy-alias")
}

func TestPruneLegacyRunWithUnknownPolicyDeletesNothing(t *testing.T) {
	fake, _ := newFakeConsul(t)
	current := ServiceTokenAccessorID(testService, fakeNodeName)
	fake.add(&api.ACLToken{AccessorID: current})
	fake.add(&api.ACLToken{AccessorID: "legacy", Description: "Token for carbonio-files/anything"})

	cmd := &PruneLegacyServiceTokens{Service: testService, Policy: "missing-policy"}

	require.NoError(t, cmd.Run(nil))
	assert.Contains(t, fake.tokens, "legacy")
}

func TestPruneLegacyRunRefusesBeforeEnsure(t *testing.T) {
	fake, _ := newFakeConsul(t)

	hostname, err := os.Hostname()
	require.NoError(t, err)

	fake.add(&api.ACLToken{AccessorID: "legacy-local", Description: "Token for carbonio-files/" + hostname})

	err = (&PruneLegacyServiceTokens{Service: testService, Policy: testPolicy}).Run(nil)

	require.ErrorIs(t, err, errServiceTokenNotEnsured)
	assert.Contains(t, fake.tokens, "legacy-local")
}

func TestEnsureRunReportsUnhealthyConsulAsSuch(t *testing.T) {
	fake, _ := newFakeConsul(t)
	fake.noLeader = true

	err := (&EnsureServiceToken{
		Service:   testService,
		Policy:    testPolicy,
		TokenFile: filepath.Join(t.TempDir(), "token"),
	}).Run(nil)

	require.ErrorContains(t, err, "unexpected response from Consul")
	assert.NotContains(t, err.Error(), "not a valid Consul token")
}
