// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package legacytoken_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/legacytoken"
)

func newConsulACL(t *testing.T, handler http.HandlerFunc) legacytoken.ACL {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := api.DefaultConfig()
	cfg.Address = server.URL

	client, err := api.NewClient(cfg)
	require.NoError(t, err)

	return legacytoken.NewClientACL(client)
}

func TestClientACL_tokenList(t *testing.T) {
	acl := newConsulACL(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/acl/tokens", r.URL.Path)
		_, _ = w.Write([]byte(`[{"AccessorID":"a1","Description":"d1"},{"AccessorID":"a2"}]`))
	})

	tokens, err := acl.TokenList()
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	assert.Equal(t, "a1", tokens[0].AccessorID)
	assert.Equal(t, "d1", tokens[0].Description)

	failing := newConsulACL(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ACL not found", http.StatusForbidden)
	})

	_, err = failing.TokenList()
	require.ErrorContains(t, err, "403")
}

func TestClientACL_tokenDelete(t *testing.T) {
	var method, path string

	acl := newConsulACL(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte("true"))
	})

	require.NoError(t, acl.TokenDelete("abc"))
	assert.Equal(t, http.MethodDelete, method)
	assert.Equal(t, "/v1/acl/token/abc", path)

	failing := newConsulACL(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	})

	require.ErrorContains(t, failing.TokenDelete("abc"), "403")
}
