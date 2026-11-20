// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// meshReply is the answer of the Consul stub to a request.
type meshReply struct {
	status int
	body   string
}

// meshRequest is a request received by the Consul stub.
type meshRequest struct {
	method, path, query, token, body string
}

// meshConsul emulates the HTTP API of a Consul agent: the replies are looked up by "METHOD /path", anything else
// is a 404 as Consul answers for an unknown object.
type meshConsul struct {
	mu       sync.Mutex
	replies  map[string]meshReply
	requests []meshRequest
}

// newMeshConsul starts the stub and points the Consul client at it. The caller must not run in parallel: the
// environment is the one of the process.
func newMeshConsul(t *testing.T, replies map[string]meshReply) *meshConsul {
	t.Helper()

	stub := &meshConsul{replies: replies}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		stub.mu.Lock()
		stub.requests = append(stub.requests, meshRequest{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery,
			token: r.Header.Get("X-Consul-Token"), body: string(body),
		})
		reply, known := stub.replies[r.Method+" "+r.URL.Path]
		stub.mu.Unlock()

		if !known {
			reply = meshReply{status: http.StatusNotFound, body: "not found"}
		}

		w.WriteHeader(reply.status)
		_, _ = io.WriteString(w, reply.body)
	}))
	t.Cleanup(server.Close)

	for _, name := range []string{
		"CONSUL_HTTP_TOKEN", "CONSUL_HTTP_TOKEN_FILE", "CONSUL_HTTP_AUTH", "CONSUL_HTTP_SSL", "CONSUL_CACERT",
		"CONSUL_CAPATH", "CONSUL_CLIENT_CERT", "CONSUL_CLIENT_KEY", "CONSUL_TLS_SERVER_NAME",
		"CONSUL_HTTP_SSL_VERIFY", "CONSUL_NAMESPACE", "CONSUL_PARTITION",
	} {
		t.Setenv(name, "")
	}

	t.Setenv("CONSUL_HTTP_ADDR", strings.TrimPrefix(server.URL, "http://"))

	return stub
}

func (s *meshConsul) last(t *testing.T) meshRequest {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	require.NotEmpty(t, s.requests)

	return s.requests[len(s.requests)-1]
}

func (s *meshConsul) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.requests)
}

const (
	meshTokenSelf = "GET /v1/acl/token/self"
	meshOK        = http.StatusOK
	meshDown      = http.StatusInternalServerError
)

func TestNewCluster_checksTheToken(t *testing.T) {
	tests := []struct {
		name    string
		reply   meshReply
		wantErr string
	}{
		{"valid", meshReply{meshOK, `{"SecretID":"boot"}`}, ""},
		{"refused", meshReply{http.StatusForbidden, "ACL not found"}, "the Consul token is not valid"},
		{"unauthorized", meshReply{http.StatusUnauthorized, "no token"}, "the Consul token is not valid"},
		{"agent failing", meshReply{meshDown, "no leader"}, "unable to reach Consul"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newMeshConsul(t, map[string]meshReply{meshTokenSelf: tt.reply})

			cluster, err := NewCluster("boot")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Nil(t, cluster)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, cluster)
			}

			assert.Equal(t, "boot", stub.last(t).token)
		})
	}
}

func TestNewCluster_rejectsAnUnusableClientConfiguration(t *testing.T) {
	newMeshConsul(t, nil)
	t.Setenv("CONSUL_CACERT", "/nonexistent/ca.pem")

	_, err := NewCluster("boot")
	require.Error(t, err)

	_, err = newConsulLogin("spire")
	require.Error(t, err)

	_, err = newConsulService("token")
	require.Error(t, err)
}

func TestConsulCluster_operations(t *testing.T) {
	tests := []struct {
		name   string
		route  string
		run    func(c *consulCluster) error
		reply  string
		method string
		inBody string
	}{
		{
			"policy by name", "GET /v1/acl/policy/name/p1",
			func(c *consulCluster) error {
				policy, err := c.PolicyByName("p1")
				if err == nil {
					assert.Equal(t, "p1", policy.Name)
				}

				return err
			},
			`{"Name":"p1"}`, http.MethodGet, "",
		},
		{
			"policy create", "PUT /v1/acl/policy",
			func(c *consulCluster) error { return c.PolicyCreate(&api.ACLPolicy{Name: "p1", Rules: "r"}) },
			`{"ID":"id","Name":"p1"}`, http.MethodPut, `"Name":"p1"`,
		},
		{
			"policy update", "PUT /v1/acl/policy/id1",
			func(c *consulCluster) error { return c.PolicyUpdate(&api.ACLPolicy{ID: "id1", Name: "p1"}) },
			`{"ID":"id1","Name":"p1"}`, http.MethodPut, `"Name":"p1"`,
		},
		{
			"role by name", "GET /v1/acl/role/name/r1",
			func(c *consulCluster) error {
				role, err := c.RoleByName("r1")
				if err == nil {
					assert.Equal(t, "r1", role.Name)
				}

				return err
			},
			`{"Name":"r1"}`, http.MethodGet, "",
		},
		{
			"role create", "PUT /v1/acl/role",
			func(c *consulCluster) error { return c.RoleCreate(&api.ACLRole{Name: "r1"}) },
			`{"ID":"id","Name":"r1"}`, http.MethodPut, `"Name":"r1"`,
		},
		{
			"role update", "PUT /v1/acl/role/id1",
			func(c *consulCluster) error { return c.RoleUpdate(&api.ACLRole{ID: "id1", Name: "r1"}) },
			`{"ID":"id1","Name":"r1"}`, http.MethodPut, `"Name":"r1"`,
		},
		{
			"binding rules", "GET /v1/acl/binding-rules",
			func(c *consulCluster) error {
				rules, err := c.BindingRules("spire")
				if err == nil {
					assert.Len(t, rules, 1)
				}

				return err
			},
			`[{"ID":"b1","AuthMethod":"spire"}]`, http.MethodGet, "",
		},
		{
			"binding rule create", "PUT /v1/acl/binding-rule",
			func(c *consulCluster) error { return c.BindingRuleCreate(&api.ACLBindingRule{AuthMethod: "spire"}) },
			`{"ID":"b1"}`, http.MethodPut, `"AuthMethod":"spire"`,
		},
		{
			"binding rule update", "PUT /v1/acl/binding-rule/b1",
			func(c *consulCluster) error {
				return c.BindingRuleUpdate(&api.ACLBindingRule{ID: "b1", AuthMethod: "spire"})
			},
			`{"ID":"b1"}`, http.MethodPut, `"AuthMethod":"spire"`,
		},
		{
			"config entry", "GET /v1/config/service-defaults/files",
			func(c *consulCluster) error {
				entry, err := c.ConfigEntry(api.ServiceDefaults, "files")
				if err == nil {
					assert.Equal(t, "files", entry.GetName())
				}

				return err
			},
			`{"Kind":"service-defaults","Name":"files","Protocol":"http"}`, http.MethodGet, "",
		},
		{
			"set config entry", "PUT /v1/config",
			func(c *consulCluster) error {
				return c.SetConfigEntry(&api.ServiceConfigEntry{Kind: api.ServiceDefaults, Name: "files"})
			},
			`true`, http.MethodPut, `"Name":"files"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newMeshConsul(t, map[string]meshReply{
				meshTokenSelf: {meshOK, `{}`},
				tt.route:      {meshOK, tt.reply},
			})

			cluster, err := NewCluster("boot")
			require.NoError(t, err)

			consul, isConsul := cluster.(*consulCluster)
			require.True(t, isConsul)

			require.NoError(t, tt.run(consul))

			req := stub.last(t)
			assert.Equal(t, tt.method, req.method)
			assert.Equal(t, tt.route, req.method+" "+req.path)
			assert.Equal(t, "boot", req.token)
			assert.Contains(t, req.body, tt.inBody)

			// The same call against a failing agent reports the failure.
			stub.mu.Lock()
			stub.replies[tt.route] = meshReply{meshDown, "boom"}
			stub.mu.Unlock()

			require.Error(t, tt.run(consul))
		})
	}
}

func TestConsulCluster_missingObjectsAreNil(t *testing.T) {
	newMeshConsul(t, map[string]meshReply{meshTokenSelf: {meshOK, `{}`}})

	cluster, err := NewCluster("boot")
	require.NoError(t, err)

	policy, err := cluster.PolicyByName("none")
	require.NoError(t, err)
	assert.Nil(t, policy)

	role, err := cluster.RoleByName("none")
	require.NoError(t, err)
	assert.Nil(t, role)

	entry, err := cluster.ConfigEntry(api.ServiceDefaults, "none")
	require.NoError(t, err)
	assert.Nil(t, entry)
}

func TestConsulLogin(t *testing.T) {
	stub := newMeshConsul(t, map[string]meshReply{
		"POST /v1/acl/login":  {meshOK, `{"SecretID":"issued"}`},
		"POST /v1/acl/logout": {meshOK, ``},
	})

	login, err := newConsulLogin("spire")
	require.NoError(t, err)

	secret, err := login.Login("a.jwt.token")
	require.NoError(t, err)
	assert.Equal(t, "issued", secret)
	assert.Contains(t, stub.last(t).body, `"AuthMethod":"spire"`)
	assert.Contains(t, stub.last(t).body, `"BearerToken":"a.jwt.token"`)

	require.NoError(t, login.Logout("issued"))
	assert.Equal(t, "issued", stub.last(t).token)

	stub.mu.Lock()
	stub.replies["POST /v1/acl/login"] = meshReply{http.StatusForbidden, "denied"}
	stub.replies["POST /v1/acl/logout"] = meshReply{meshDown, "boom"}
	stub.mu.Unlock()

	_, err = login.Login("bad")
	require.Error(t, err)
	require.Error(t, login.Logout("issued"))
}

func TestConsulLogin_valid(t *testing.T) {
	tests := []struct {
		name      string
		reply     meshReply
		wantValid bool
		wantErr   bool
	}{
		{"accepted", meshReply{meshOK, `{"SecretID":"s"}`}, true, false},
		{"forbidden", meshReply{http.StatusForbidden, "ACL not found"}, false, false},
		{"unauthorized", meshReply{http.StatusUnauthorized, "denied"}, false, false},
		{"agent failing", meshReply{meshDown, "no leader"}, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newMeshConsul(t, map[string]meshReply{meshTokenSelf: tt.reply})

			login, err := newConsulLogin("spire")
			require.NoError(t, err)

			valid, err := login.Valid("secret")
			assert.Equal(t, tt.wantValid, valid)
			assert.Equal(t, tt.wantErr, err != nil)
			assert.Equal(t, "secret", stub.last(t).token)
		})
	}
}

func TestConsulLogin_isLogin(t *testing.T) {
	tests := []struct {
		name    string
		reply   meshReply
		want    bool
		wantErr bool
	}{
		{"created by a login", meshReply{meshOK, `{"AuthMethod":"spire"}`}, true, false},
		{"created by hand", meshReply{meshOK, `{"AuthMethod":""}`}, false, false},
		{"unknown token", meshReply{http.StatusForbidden, "ACL not found"}, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newMeshConsul(t, map[string]meshReply{meshTokenSelf: tt.reply})

			login, err := newConsulLogin("spire")
			require.NoError(t, err)

			got, err := login.IsLogin("secret")
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantErr, err != nil)
		})
	}
}

func TestIsDenied(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"forbidden", api.StatusError{Code: http.StatusForbidden}, true},
		{"unauthorized", api.StatusError{Code: http.StatusUnauthorized}, true},
		{"server error", api.StatusError{Code: http.StatusInternalServerError}, false},
		{"not a status", errCluster, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isDenied(tt.err))
		})
	}
}

func TestConsulService_keyValue(t *testing.T) {
	stub := newMeshConsul(t, map[string]meshReply{
		"GET /v1/kv/present":    {meshOK, `[{"Key":"present","Value":"dmFsdWU="}]`},
		"PUT /v1/kv/present":    {meshOK, `true`},
		"DELETE /v1/kv/present": {meshOK, `true`},
	})

	service, err := newConsulService("svc-token")
	require.NoError(t, err)

	value, found, err := service.Get("present")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "value", value)
	assert.Equal(t, "svc-token", stub.last(t).token)

	_, found, err = service.Get("absent")
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, service.Put("present", "new"))
	assert.Equal(t, "new", stub.last(t).body)

	require.NoError(t, service.Delete("present"))
	assert.Equal(t, http.MethodDelete, stub.last(t).method)

	stub.mu.Lock()
	stub.replies["GET /v1/kv/present"] = meshReply{meshDown, "boom"}
	stub.replies["PUT /v1/kv/present"] = meshReply{meshDown, "boom"}
	stub.replies["DELETE /v1/kv/present"] = meshReply{meshDown, "boom"}
	stub.mu.Unlock()

	_, _, err = service.Get("present")
	require.Error(t, err)
	require.Error(t, service.Put("present", "new"))
	require.Error(t, service.Delete("present"))
}

func TestConsulService_instances(t *testing.T) {
	stub := newMeshConsul(t, map[string]meshReply{
		"GET /v1/health/service/files": {meshOK, `[
			{"Node":{"Address":"10.0.0.1"},"Service":{"Address":"10.0.0.9","Meta":{"zone":"a"}}},
			{"Node":{"Address":"10.0.0.2"},"Service":{"Address":""}}
		]`},
	})

	service, err := newConsulService("svc-token")
	require.NoError(t, err)

	instances, err := service.Instances("files", true)
	require.NoError(t, err)
	assert.Equal(t, []Instance{
		{Address: "10.0.0.9", Meta: map[string]string{"zone": "a"}},
		{Address: "10.0.0.2"},
	}, instances)
	assert.Contains(t, stub.last(t).query, "passing")

	_, err = service.Instances("unknown", false)
	require.Error(t, err)
}

func TestConsulService_setConfigEntry(t *testing.T) {
	stub := newMeshConsul(t, map[string]meshReply{"PUT /v1/config": {meshOK, `true`}})

	service, err := newConsulService("svc-token")
	require.NoError(t, err)

	entry := &api.ServiceConfigEntry{Kind: api.ServiceDefaults, Name: "files"}
	require.NoError(t, service.SetConfigEntry(entry))
	assert.Equal(t, "svc-token", stub.last(t).token)
	assert.Contains(t, stub.last(t).body, `"Name":"files"`)

	stub.mu.Lock()
	stub.replies["PUT /v1/config"] = meshReply{http.StatusForbidden, "denied"}
	stub.mu.Unlock()

	require.Error(t, service.SetConfigEntry(entry))
	assert.Equal(t, 2, stub.count())
}
