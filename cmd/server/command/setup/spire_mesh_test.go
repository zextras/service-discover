// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFlowEntry = errors.New("spire-server entry create failed")

// meshConsul plays the ACL and config entry endpoints of Consul for a cluster where nothing is registered yet, and
// records the writes.
type meshConsul struct {
	mu        sync.Mutex
	denyToken bool
	writes    []string
}

func (m *meshConsul) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := r.URL.Path

	switch {
	case path == "/v1/acl/token/self" && m.denyToken:
		http.Error(w, "ACL not found", http.StatusForbidden)
	case path == "/v1/acl/token/self":
		_, _ = w.Write([]byte(`{"AccessorID":"a","SecretID":"s"}`))
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/acl/binding-rules"):
		_, _ = w.Write([]byte(`[]`))
	case r.Method == http.MethodGet:
		// Policies, roles and config entries: none exists.
		http.Error(w, "not found", http.StatusNotFound)
	case path == "/v1/config":
		m.writes = append(m.writes, "config")
		_, _ = w.Write([]byte(`true`))
	default:
		m.writes = append(m.writes, strings.TrimPrefix(path, "/v1/acl/"))
		_, _ = w.Write([]byte(`{"ID":"id","Name":"name"}`))
	}
}

func newMeshConsul(t *testing.T) *meshConsul {
	t.Helper()

	fake := &meshConsul{}
	server := httptest.NewServer(fake)

	t.Cleanup(server.Close)
	t.Setenv("CONSUL_HTTP_ADDR", strings.TrimPrefix(server.URL, "http://"))

	return fake
}

func meshDeps(created *[]string) *flowDeps {
	return &flowDeps{handler: func(args []string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "entry show") {
			return []byte(`{"entries":[]}`), nil
		}

		*created = append(*created, strings.Join(args, " "))

		return []byte("created"), nil
	}}
}

func TestRegisterMeshServices_registersTheClusterAndTheSpireEntriesOfTheServer(t *testing.T) {
	fake := newMeshConsul(t)

	var created []string

	require.NoError(t, registerMeshServices(meshDeps(&created), flowHostname, "root-token"))

	assert.Contains(t, fake.writes, "policy")
	assert.Contains(t, fake.writes, "role")
	assert.Contains(t, fake.writes, "binding-rule")
	assert.Contains(t, fake.writes, "config")

	require.NotEmpty(t, created, "the SPIRE entries of the services of the server are created")
	assert.Contains(t, strings.Join(created, "\n"), "spiffe://carbonio/node/"+flowHostname)
}

func TestRegisterMeshServices_stopsWhenTheClusterRejectsTheToken(t *testing.T) {
	fake := newMeshConsul(t)
	fake.denyToken = true

	var created []string

	require.ErrorContains(t, registerMeshServices(meshDeps(&created), flowHostname, "bad"), "token is not valid")
	assert.Empty(t, created, "no SPIRE entry is created when the registration failed")
}

func TestRegisterMeshServices_reportsTheFailureToCreateTheEntries(t *testing.T) {
	newMeshConsul(t)

	deps := &flowDeps{handler: func(args []string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "entry show") {
			return []byte(`{"entries":[]}`), nil
		}

		return nil, errFlowEntry
	}}

	require.ErrorContains(t, registerMeshServices(deps, flowHostname, "root-token"), errFlowEntry.Error())
}
