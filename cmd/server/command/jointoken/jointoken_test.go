// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package jointoken

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/mesh"
	"github.com/zextras/service-discover/pkg/spire"
)

type fakeDeps struct {
	uid      int
	commands map[string]string
	calls    []string
	entries  []string // SPIFFE IDs of the entries created
}

func (f *fakeDeps) CreateCommand(_ string, args ...string) exec.Cmd {
	key := args[0] + " " + args[1]
	f.calls = append(f.calls, key)

	if key == "entry create" {
		f.entries = append(f.entries, args[spiffeIDArgument(args)])
	}

	cmd := new(mocks.Cmd)
	cmd.On("Output").Return([]byte(f.commands[key]), nil)

	return cmd
}

// spiffeIDArgument finds the SPIFFE ID of the entry in the arguments of "spire-server entry create".
func spiffeIDArgument(args []string) int {
	for i, arg := range args {
		if arg == "-spiffeID" {
			return i + 1
		}
	}

	return -1
}

func (f *fakeDeps) GetuidSyscall() int { return f.uid }

func (f *fakeDeps) ServerHostname() (string, error) { return "mail.example.com", nil }

func (f *fakeDeps) ConsulServers() ([]string, error) { return []string{"10.0.0.1:8300"}, nil }

func newCommand(t *testing.T, deps *fakeDeps, host string) (*JoinToken, *bytes.Buffer) {
	t.Helper()

	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, command.ConsulCA), []byte("CA-PEM\n"), 0600))

	out := &bytes.Buffer{}

	return &JoinToken{Host: host, TTL: 600_000_000_000, consulHome: home, deps: deps, writer: out}, out
}

func newDeps(entries string) *fakeDeps {
	return &fakeDeps{commands: map[string]string{
		"entry show":     entries,
		"entry create":   "created",
		"token generate": `{"value":"join-me"}`,
		"bundle show":    "TRUST-BUNDLE\n",
	}}
}

func TestJoinToken_plainOutputIsTheBundle(t *testing.T) {
	t.Parallel()

	deps := newDeps(`{"entries":[]}`)
	cmd, out := newCommand(t, deps, "mail2.example.com")

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{Format: formatter.PlainFormatOutput}))

	bundle, err := spire.DecodeJoinBundle(out.String())
	require.NoError(t, err)
	assert.Equal(t, &spire.JoinBundle{
		Version:       1,
		TrustDomain:   "carbonio",
		SpireServer:   "mail.example.com:8081",
		SpireBundle:   "TRUST-BUNDLE\n",
		JoinToken:     "join-me",
		ConsulCA:      "CA-PEM\n",
		ConsulServers: []string{"10.0.0.1:8300"},
		NodeName:      "agent-mail2-example-com",
	}, bundle)

	defs, err := mesh.Load()
	require.NoError(t, err)

	// the entry of the Consul agent, one for each service of the mesh, then the token and the bundle
	want := []string{"entry show", "entry create"}
	for range defs {
		want = append(want, "entry show", "entry create")
	}

	want = append(want, "token generate", "bundle show")

	assert.Equal(t, want, deps.calls)
	assert.Contains(t, deps.entries, "spiffe://carbonio/service/carbonio-files")
	assert.Len(t, deps.entries, len(defs)+1)
}

func TestJoinToken_jsonOutput(t *testing.T) {
	t.Parallel()

	cmd, out := newCommand(t, newDeps(`{"entries":[]}`), "mail2.example.com")

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{Format: formatter.JSONFormatOutput}))

	got := map[string]string{}
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "agent-mail2-example-com", got["node_name"])
	assert.Equal(t, "spiffe://carbonio/node/mail2.example.com", got["spiffe_id"])

	_, err := spire.DecodeJoinBundle(got["join_bundle"])
	require.NoError(t, err)
}

func TestJoinToken_existingEntryIsNotRecreated(t *testing.T) {
	t.Parallel()

	deps := newDeps(`{"entries":[{"id":"1"}]}`)
	cmd, _ := newCommand(t, deps, "mail2.example.com")

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}))
	assert.NotContains(t, deps.calls, "entry create")
	assert.Contains(t, deps.calls, "token generate", "a fresh token is generated at every run")
}

func TestJoinToken_rejectsInvalidInput(t *testing.T) {
	t.Parallel()

	nonRoot := newDeps(`{"entries":[]}`)
	nonRoot.uid = 1000
	cmd, _ := newCommand(t, nonRoot, "mail2.example.com")
	require.ErrorContains(t, cmd.Run(&command.GlobalCommonFlags{}), "root")
	assert.Empty(t, nonRoot.calls)

	for _, host := range []string{"", "a b", "a/b", "../x", "-host"} {
		deps := newDeps(`{"entries":[]}`)
		cmd, _ := newCommand(t, deps, host)
		require.ErrorIs(t, cmd.Run(&command.GlobalCommonFlags{}), errInvalidHost, host)
		assert.Empty(t, deps.calls)
	}
}

// failingDeps makes the named dependency fail on top of fakeDeps.
type failingDeps struct {
	fakeDeps

	failHostname bool
	failServers  bool
	failCommand  string
}

func (f *failingDeps) ServerHostname() (string, error) {
	if f.failHostname {
		return "", errors.New("no localconfig")
	}

	return f.fakeDeps.ServerHostname()
}

func (f *failingDeps) ConsulServers() ([]string, error) {
	if f.failServers {
		return nil, errors.New("agent unreachable")
	}

	return f.fakeDeps.ConsulServers()
}

func (f *failingDeps) CreateCommand(name string, args ...string) exec.Cmd {
	key := args[0] + " " + args[1]
	if key != f.failCommand {
		return f.fakeDeps.CreateCommand(name, args...)
	}

	cmd := new(mocks.Cmd)
	cmd.On("Output").Return(nil, errors.New("spire-server failed"))

	return cmd
}

func TestJoinToken_failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		deps    *failingDeps
		noCA    bool
		wantErr string
	}{
		{name: "server hostname", deps: &failingDeps{failHostname: true}, wantErr: "no localconfig"},
		{name: "missing Consul CA", deps: &failingDeps{}, noCA: true, wantErr: "unable to read the Consul CA"},
		{name: "consul servers", deps: &failingDeps{failServers: true},
			wantErr: "unable to get the Consul servers from the local agent: agent unreachable"},
		{name: "entry lookup", deps: &failingDeps{failCommand: "entry show"}, wantErr: "spire-server failed"},
		{name: "token generation", deps: &failingDeps{failCommand: "token generate"}, wantErr: "spire-server failed"},
		{name: "bundle", deps: &failingDeps{failCommand: "bundle show"}, wantErr: "spire-server failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.deps.fakeDeps = *newDeps(`{"entries":[]}`)
			cmd, out := newCommand(t, &fakeDeps{}, "mail2.example.com")
			cmd.deps = tt.deps

			if tt.noCA {
				require.NoError(t, os.Remove(filepath.Join(cmd.consulHome, command.ConsulCA)))
			}

			err := cmd.Run(&command.GlobalCommonFlags{})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, out.String(), "no bundle is printed on failure")
		})
	}
}

func TestRealDependencies(t *testing.T) {
	t.Run("hostname comes from the localconfig", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "localconfig.xml")
		require.NoError(t, os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<localconfig>
  <key name="ldap_master_url"><value>ldap://mail.example.com:389</value></key>
  <key name="ldap_url"><value>ldap://mail.example.com:389</value></key>
  <key name="zimbra_ldap_userdn"><value>uid=zimbra,cn=admins,cn=zimbra</value></key>
  <key name="zimbra_ldap_password"><value>password</value></key>
  <key name="zimbra_server_hostname"><value>mail.example.com</value></key>
</localconfig>`), 0600))

		hostname, err := realDependencies{localConfigPath: path}.ServerHostname()
		require.NoError(t, err)
		assert.Equal(t, "mail.example.com", hostname)

		_, err = realDependencies{localConfigPath: filepath.Join(t.TempDir(), "missing.xml")}.ServerHostname()
		require.Error(t, err)
	})

	t.Run("consul servers are the raft peers of the local agent", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/status/peers", r.URL.Path)

			_, _ = w.Write([]byte(`["10.0.0.1:8300","10.0.0.2:8300"]`))
		}))
		defer server.Close()

		t.Setenv("CONSUL_HTTP_ADDR", strings.TrimPrefix(server.URL, "http://"))
		t.Setenv("CONSUL_HTTP_SSL", "false")

		servers, err := realDependencies{}.ConsulServers()
		require.NoError(t, err)
		assert.Equal(t, []string{"10.0.0.1:8300", "10.0.0.2:8300"}, servers)
	})

	t.Run("consul servers fail when the agent is down", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		addr := strings.TrimPrefix(server.URL, "http://")
		server.Close()

		t.Setenv("CONSUL_HTTP_ADDR", addr)

		_, err := realDependencies{}.ConsulServers()
		require.Error(t, err)
	})

	t.Run("commands and uid are the real ones", func(t *testing.T) {
		out, err := realDependencies{}.CreateCommand("echo", "hello").Output()
		require.NoError(t, err)
		assert.Equal(t, "hello\n", string(out))
		assert.Equal(t, os.Getuid(), realDependencies{}.GetuidSyscall())
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := New(out)

	assert.Equal(t, config.ConsulHome, cmd.consulHome)
	assert.Equal(t, realDependencies{localConfigPath: config.LocalConfigPath}, cmd.deps)
	assert.Same(t, out, cmd.writer)
}
