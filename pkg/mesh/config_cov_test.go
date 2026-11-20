// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/spire"
)

func TestSPIREMode_followsTheAgentConfiguration(t *testing.T) {
	t.Parallel()

	_, err := os.Stat(filepath.Join(spire.ConfigDir, spire.AgentConfFile))

	assert.Equal(t, err == nil, SPIREMode())
}

func TestConsulConfigArgs_failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, machine *fakeMachine)
		noArgs  bool
		wantErr string
	}{
		{
			name:    "no Consul user",
			prepare: func(_ *testing.T, machine *fakeMachine) { delete(machine.users, ConsulUser) },
			noArgs:  true,
			wantErr: "unable to find the Consul user",
		},
		{
			name: "rendered directory cannot be created",
			prepare: func(t *testing.T, machine *fakeMachine) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(machine.env.RenderedDir), "file"), nil, 0o600))
				machine.env.RenderedDir = filepath.Join(filepath.Dir(machine.env.RenderedDir), "file", "rendered")
			},
			noArgs:  true,
			wantErr: "not a directory",
		},
		{
			name: "rendered directory cannot be owned by Consul",
			prepare: func(_ *testing.T, machine *fakeMachine) {
				machine.env.Chown = func(string, int, int) error { return errCluster }
			},
			wantErr: errCluster.Error(),
		},
		{
			name: "state directory is not writable",
			prepare: func(_ *testing.T, machine *fakeMachine) {
				machine.env.StateDir = filepath.Join(machine.env.StateDir, "missing")
			},
			wantErr: "no such file or directory",
		},
		{
			name: "sidecars marker cannot be written",
			prepare: func(t *testing.T, machine *fakeMachine) {
				t.Helper()
				require.NoError(t, os.Mkdir(machine.env.sidecarsMarker(), 0o755))
			},
			wantErr: "is a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			tt.prepare(t, machine)

			args, err := ConsulConfigArgs(machine.env, loadDefs(t))
			require.ErrorContains(t, err, tt.wantErr)

			if tt.noArgs {
				assert.Nil(t, args)
				require.ErrorIs(t, err, errNoRenderedDir)
			} else {
				assert.Equal(t, []string{"-config-dir", machine.env.RenderedDir}, args)
			}
		})
	}
}

func TestNoteServiceSet_unreadableDirectory(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("the permissions do not apply to root")
	}

	machine := newMachine(t)
	require.NoError(t, os.MkdirAll(machine.env.RenderedDir, 0o755))
	require.NoError(t, os.Chmod(machine.env.RenderedDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(machine.env.RenderedDir, 0o755) })

	require.ErrorIs(t, noteServiceSet(machine.env), os.ErrPermission)
}
