// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package meshcmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
)

var errSync = errors.New("sync failed")

type recorder struct {
	exits    []int
	synced   [][]string
	commands []string
	syncErr  error
	runErr   error
}

func (r *recorder) command(script string, spire bool, uid int) (*PendingSetup, *bytes.Buffer) {
	out := &bytes.Buffer{}

	return &PendingSetup{
		Script:    script,
		writer:    out,
		getuid:    func() int { return uid },
		spireMode: func() bool { return spire },
		exit:      func(code int) { r.exits = append(r.exits, code) },
		run: func(name string) error {
			r.commands = append(r.commands, name)

			return r.runErr
		},
		sync: func(services []string) error {
			r.synced = append(r.synced, services)

			return r.syncErr
		},
	}, out
}

func TestPendingSetup_nodesNotUsingSpireRunTheScript(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	cmd, _ := rec.command("511-carbonio-files.sh", false, 0)

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}))
	assert.Equal(t, []int{ExitRunScript}, rec.exits)
	assert.Empty(t, rec.synced)
}

func TestPendingSetup_scriptsNotCoveredRunTheScript(t *testing.T) {
	t.Parallel()

	for _, script := range []string{"100-unknown.sh", "300-carbonio-docs-editor.sh"} {
		rec := &recorder{}
		cmd, _ := rec.command(script, true, 0)

		require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}), script)
		assert.Equal(t, []int{ExitRunScript}, rec.exits, script)
		assert.Empty(t, rec.synced, script)
	}
}

func TestPendingSetup_replacedScriptSyncsAndRunsThePostSetup(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	cmd, out := rec.command("720-carbonio-message-broker.sh", true, 0)

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}))
	assert.Empty(t, rec.exits, "the script is not run")
	assert.Equal(t, [][]string{{"carbonio-message-broker"}}, rec.synced)
	assert.Equal(t, []string{"carbonio-message-broker-setup-users"}, rec.commands)
	assert.Contains(t, out.String(), "managed through SPIRE")
}

func TestPendingSetup_failures(t *testing.T) {
	t.Parallel()

	rec := &recorder{syncErr: errSync}
	cmd, _ := rec.command("720-carbonio-message-broker.sh", true, 0)

	require.ErrorIs(t, cmd.Run(&command.GlobalCommonFlags{}), errSync)
	assert.Empty(t, rec.commands, "the post setup needs the token")

	rec = &recorder{runErr: errSync}
	cmd, _ = rec.command("720-carbonio-message-broker.sh", true, 0)
	require.ErrorIs(t, cmd.Run(&command.GlobalCommonFlags{}), errSync)

	rec = &recorder{}
	cmd, _ = rec.command("720-carbonio-message-broker.sh", true, 1000)
	require.ErrorIs(t, cmd.Run(&command.GlobalCommonFlags{}), errNotRoot)
	assert.Empty(t, rec.synced)
}

func TestSync_notOnSpireNodes(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := Sync{writer: out, getuid: func() int { return 1000 }, spireMode: func() bool { return false }}

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}))
	assert.Contains(t, out.String(), "does not use SPIRE")
}

func TestSync_mustBeRoot(t *testing.T) {
	t.Parallel()

	cmd := Sync{writer: &bytes.Buffer{}, getuid: func() int { return 1000 }, spireMode: func() bool { return true }}

	require.ErrorIs(t, cmd.Run(&command.GlobalCommonFlags{}), errNotRoot)
}
