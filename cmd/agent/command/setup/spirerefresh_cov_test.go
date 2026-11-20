// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/spire"
)

func TestRefresh_run(t *testing.T) {
	flags := func(format formatter.OutputFormat) *command.GlobalCommonFlags {
		return &command.GlobalCommonFlags{Format: format}
	}

	peers := []string{"10.0.0.1:8300", "10.0.0.2:8300"}

	t.Run("plain output", func(t *testing.T) {
		f := newRefreshFixture(t, spireServersAlias, peers, "10.0.0.1", "10.0.0.2")
		out := &bytes.Buffer{}

		require.NoError(t, (&Refresh{setup: *f.setup}).run(flags(formatter.PlainFormatOutput), f.deps, out))
		assert.Equal(t, spireServersAlias+" now resolves to 10.0.0.1, 10.0.0.2\n", out.String())
	})

	t.Run("json output", func(t *testing.T) {
		f := newRefreshFixture(t, spireServersAlias, peers, "10.0.0.1")
		out := &bytes.Buffer{}

		require.NoError(t, (&Refresh{setup: *f.setup}).run(flags(formatter.JSONFormatOutput), f.deps, out))
		assert.JSONEq(t, `{"steps":["`+spireServersAlias+` now resolves to 10.0.0.1"]}`, out.String())
	})

	t.Run("invalid output format", func(t *testing.T) {
		f := newRefreshFixture(t, spireServersAlias, peers, "10.0.0.1")

		err := (&Refresh{setup: *f.setup}).run(flags(formatter.OutputFormat(99)), f.deps, &bytes.Buffer{})
		require.ErrorContains(t, err, "formatting option is not valid")
	})

	t.Run("not root", func(t *testing.T) {
		f := newRefreshFixture(t, spireServersAlias, peers, "10.0.0.1")
		f.deps.ExpectedCalls = append([]*mock.Call{f.deps.On("GetuidSyscall").Return(1000)},
			f.deps.ExpectedCalls[:len(f.deps.ExpectedCalls)-1]...)

		err := (&Refresh{setup: *f.setup}).run(flags(formatter.PlainFormatOutput), f.deps, &bytes.Buffer{})
		require.ErrorContains(t, err, "must be executed as root")
	})

	t.Run("refresh error", func(t *testing.T) {
		f := newRefreshFixture(t, spireServersAlias, peers)

		err := (&Refresh{setup: *f.setup}).run(flags(formatter.PlainFormatOutput), f.deps, &bytes.Buffer{})
		require.ErrorContains(t, err, "no Consul server answers")
	})
}

func TestNewSpire_usesTheInstallationPaths(t *testing.T) {
	spireCommand := NewSpire()

	assert.Equal(t, spire.ConfigDir, spireCommand.Servers.Refresh.setup.SpireConfigDir)
}

func TestRefreshOutput_json(t *testing.T) {
	rendered, err := (&refreshOutput{Steps: []string{"a", "b"}}).JSONRender()
	require.NoError(t, err)
	assert.JSONEq(t, `{"steps":["a","b"]}`, rendered)
}

func TestSetup_refreshSpireServers_failures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, f *joinFixture)
		peers   func() ([]string, error)
		wantErr string
	}{
		{
			name: "node not joined",
			prepare: func(t *testing.T, f *joinFixture) {
				require.NoError(t, os.Remove(filepath.Join(f.dirs.spire, spire.AgentConfFile)))
			},
			wantErr: "was this node joined?",
		},
		{
			name: "no server address",
			prepare: func(t *testing.T, f *joinFixture) {
				require.NoError(t, os.WriteFile(filepath.Join(f.dirs.spire, spire.AgentConfFile), []byte("agent {}\n"), 0600))
			},
			wantErr: "unable to read the SPIRE server address",
		},
		{
			name: "port too large",
			prepare: func(t *testing.T, f *joinFixture) {
				conf := "server_address = \"" + spireServersAlias + "\"\nserver_port = \"99999999999999999999\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(f.dirs.spire, spire.AgentConfFile), []byte(conf), 0600))
			},
			wantErr: "value out of range",
		},
		{
			name:    "consul agent not answering",
			peers:   func() ([]string, error) { return nil, errors.New("connection refused") },
			wantErr: "unable to list the Consul servers",
		},
		{
			name: "hosts file cannot be updated",
			prepare: func(t *testing.T, f *joinFixture) {
				require.NoError(t, os.Remove(f.dirs.hosts))
				require.NoError(t, os.MkdirAll(f.dirs.hosts, 0755))
			},
			wantErr: "unable to update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRefreshFixture(t, spireServersAlias, []string{"10.0.0.1:8300"}, "10.0.0.1")

			if tt.prepare != nil {
				tt.prepare(t, f)
			}

			if tt.peers != nil {
				consulStatusPeers = tt.peers
			}

			_, err := f.setup.refreshSpireServers(f.deps)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSetup_refreshSpireServers_peerWithoutPort(t *testing.T) {
	f := newRefreshFixture(t, spireServersAlias, []string{"10.0.0.1"}, "10.0.0.1")

	out, err := f.setup.refreshSpireServers(f.deps)
	require.NoError(t, err)

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "now resolves to 10.0.0.1")
}
