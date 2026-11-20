package main

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func authorizationConfig(t *testing.T, url, caPEM string) string {
	t.Helper()

	content, err := json.Marshal(map[string]any{
		"ports": map[string]any{"grpc_tls": 8503},
		"auto_config": map[string]any{
			"authorization": map[string]any{
				"enabled": true,
				"static":  map[string]any{"jwks_url": url, "jwks_ca_cert": caPEM},
			},
		},
	})
	assert.NoError(t, err)

	return writeMainJSON(t, string(content))
}

func newLogMock() *mockDependencies {
	m := new(mockDependencies)
	m.On("Log", mock.AnythingOfType("[]interface {}")).Return()

	return m
}

func Test_waitForJWKS(t *testing.T) {
	t.Run("waits until the endpoint answers 200, verifying it with the configured CA", func(t *testing.T) {
		var calls atomic.Int32

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) < 4 {
				w.WriteHeader(http.StatusInternalServerError)

				return
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
		mockDependencies := newLogMock()

		waitForJWKS(mockDependencies, authorizationConfig(t, server.URL+"/keys", caPEM), time.Minute, 2*time.Second)

		assert.EqualValues(t, 4, calls.Load())
		assert.Equal(t, []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second}, mockDependencies.sleeps)
		mockDependencies.AssertNumberOfCalls(t, "Log", 2) // what is awaited, then the success
	})

	t.Run("an endpoint not signed by a trusted CA is not served", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		mockDependencies := newLogMock()

		waitForJWKS(mockDependencies, authorizationConfig(t, server.URL, ""), 4*time.Second, 2*time.Second)

		assert.Equal(t, 4*time.Second, mockDependencies.elapsed)
	})

	t.Run("gives up after the window and lets consul start", func(t *testing.T) {
		var calls atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		mockDependencies := newLogMock()

		waitForJWKS(mockDependencies, authorizationConfig(t, server.URL, ""), 5*time.Second, 2*time.Second)

		assert.Equal(t, 5*time.Second, mockDependencies.elapsed)
		assert.EqualValues(t, 4, calls.Load())            // t=0, 2, 4, 5
		mockDependencies.AssertNumberOfCalls(t, "Log", 2) // what is awaited, then the give up
		assert.Contains(t, fmt.Sprint(mockDependencies.Calls[len(mockDependencies.Calls)-1].Arguments[0]), "starting consul anyway")
	})

	t.Run("does not wait without auto_config authorization", func(t *testing.T) {
		mockDependencies := new(mockDependencies) // any Log, Sleep or http call would fail the test

		for _, content := range []string{
			`{"ports": {"grpc_tls": 8503}}`,
			`{"auto_config": {"enabled": true, "server_addresses": ["10.0.0.1:8300"]}}`,
			`{"auto_config": {"authorization": {"enabled": false, "static": {"jwks_url": "http://127.0.0.1:1/keys"}}}}`,
			`{"auto_config": {"authorization": {"enabled": true, "static": {}}}}`,
			`not json`,
		} {
			waitForJWKS(mockDependencies, writeMainJSON(t, content), time.Minute, time.Second)
		}

		waitForJWKS(mockDependencies, t.TempDir()+"/missing.json", time.Minute, time.Second)

		assert.Empty(t, mockDependencies.sleeps)
		mockDependencies.AssertNotCalled(t, "Log", mock.Anything)
	})
}
