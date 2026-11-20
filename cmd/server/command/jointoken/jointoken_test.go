// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package jointoken

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/spire"
)

type fakeDeps struct {
	uid      int
	commands map[string]string
	calls    []string
}

func (f *fakeDeps) CreateCommand(_ string, args ...string) exec.Cmd {
	key := args[0] + " " + args[1]
	f.calls = append(f.calls, key)

	cmd := new(mocks.Cmd)
	cmd.On("Output").Return([]byte(f.commands[key]), nil)

	return cmd
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

	assert.Equal(t, []string{"entry show", "entry create", "token generate", "bundle show"}, deps.calls)
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
