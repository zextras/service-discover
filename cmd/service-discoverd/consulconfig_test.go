// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/mesh"
	"github.com/zextras/service-discover/pkg/spire"
)

func useSpireNode(t *testing.T, spire bool, env *mesh.Env) {
	t.Helper()

	previousMode, previousEnv := spireMode, offlineEnv
	spireMode = func() bool { return spire }
	offlineEnv = func() *mesh.Env { return env }

	t.Cleanup(func() { spireMode, offlineEnv = previousMode, previousEnv })
}

// sandboxEnv returns an offline environment working in a temporary directory, where no service is installed.
func sandboxEnv(t *testing.T) *mesh.Env {
	t.Helper()

	root := t.TempDir()
	env := mesh.NewOfflineEnv(spire.TrustDomain)
	env.ServicesDir = filepath.Join(root, "services")
	env.RenderedDir = filepath.Join(root, "rendered")
	env.StateDir = filepath.Join(root, "state")
	env.CarbonioDir = filepath.Join(root, "carbonio")
	env.SystemdDir = filepath.Join(root, "systemd")
	env.LookupUser = func(string) (mesh.Account, error) { return mesh.Account{}, nil }
	env.Chown = func(string, int, int) error { return nil }

	require.NoError(t, os.MkdirAll(env.ServicesDir, 0o755))
	require.NoError(t, os.MkdirAll(env.StateDir, 0o755))

	return env
}

func TestRealDependencies_ConsulConfigArgs(t *testing.T) {
	t.Run("a node not using SPIRE loads no configuration directory", func(t *testing.T) {
		useSpireNode(t, false, nil)

		args, err := realDependencies{}.ConsulConfigArgs()

		require.NoError(t, err)
		assert.Nil(t, args)
	})

	t.Run("a node using SPIRE loads the rendered directory", func(t *testing.T) {
		env := sandboxEnv(t)
		useSpireNode(t, true, env)

		args, err := realDependencies{}.ConsulConfigArgs()

		require.NoError(t, err)
		assert.Equal(t, []string{"-config-dir", env.RenderedDir}, args)
		assert.DirExists(t, env.RenderedDir)
	})

	t.Run("without the Consul user there is no directory to load", func(t *testing.T) {
		env := sandboxEnv(t)
		env.LookupUser = func(string) (mesh.Account, error) { return mesh.Account{}, fs.ErrNotExist }
		useSpireNode(t, true, env)

		args, err := realDependencies{}.ConsulConfigArgs()

		require.Error(t, err)
		require.True(t, errors.Is(err, fs.ErrNotExist))
		assert.Nil(t, args)
	})
}
