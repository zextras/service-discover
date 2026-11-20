// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// consulCovAnswer replaces the transport of the default HTTP client with one answering every request.
func consulCovAnswer(t *testing.T, status int, body io.Reader, err error) *[]string {
	t.Helper()

	requested := &[]string{}
	previous := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		*requested = append(*requested, request.Method+" "+request.URL.String())

		if err != nil {
			return nil, err
		}

		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Body:       io.NopCloser(body),
			Header:     http.Header{},
			Request:    request,
		}, nil
	})

	t.Cleanup(func() { http.DefaultClient.Transport = previous })

	return requested
}

func TestConsulStatusLeader(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       io.Reader
		transport  error
		wantLeader string
		wantErr    string
	}{
		{name: "answer", status: http.StatusOK, body: strings.NewReader(`"10.0.0.1:8300"`), wantLeader: "10.0.0.1:8300"},
		{name: "no leader yet", status: http.StatusOK, body: strings.NewReader(`""`)},
		{name: "not ok", status: http.StatusInternalServerError, body: strings.NewReader(""), wantErr: "answered"},
		{name: "unreachable", transport: errors.New("connection refused"), wantErr: "connection refused"},
		{name: "unreadable answer", status: http.StatusOK, body: failingBody{}, wantErr: "connection reset"},
		{name: "not json", status: http.StatusOK, body: strings.NewReader("<html>"), wantErr: "invalid character"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requested := consulCovAnswer(t, tt.status, tt.body, tt.transport)

			leader, err := consulStatusLeader()
			assert.Equal(t, tt.wantLeader, leader)
			assert.Equal(t, []string{"GET http://127.0.0.1:8500/v1/status/leader"}, *requested)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestConsulStatusPeers(t *testing.T) {
	t.Run("lists the servers", func(t *testing.T) {
		requested := consulCovAnswer(t, http.StatusOK, strings.NewReader(`["10.0.0.1:8300","10.0.0.2:8300"]`), nil)

		peers, err := consulStatusPeers()
		require.NoError(t, err)
		assert.Equal(t, []string{"10.0.0.1:8300", "10.0.0.2:8300"}, peers)
		assert.Equal(t, []string{"GET http://127.0.0.1:8500/v1/status/peers"}, *requested)
	})

	t.Run("agent not ready", func(t *testing.T) {
		consulCovAnswer(t, http.StatusServiceUnavailable, strings.NewReader(""), nil)

		_, err := consulStatusPeers()
		require.ErrorContains(t, err, "/v1/status/peers")
	})
}
