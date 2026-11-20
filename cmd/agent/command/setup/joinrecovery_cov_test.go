// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command/setup/mocks"
)

func TestSetup_joinPending_unreadableConfiguration(t *testing.T) {
	f := newJoinFixture(t, nil)
	require.NoError(t, os.WriteFile(f.setup.joinPendingPath(), nil, 0600))
	require.NoError(t, os.MkdirAll(f.setup.ConsulFileConfig, 0755))

	assert.False(t, f.setup.joinPending(), "a configuration that cannot be read is not a leftover of a join")
}

func TestSetup_cleanupJoin_reportsTheFirstError(t *testing.T) {
	f := newJoinFixture(t, nil)
	require.NoError(t, os.MkdirAll(filepath.Join(f.setup.ConsulFileConfig, "child"), 0755))
	require.NoError(t, os.WriteFile(f.setup.joinPendingPath(), nil, 0600))

	require.Error(t, f.setup.cleanupJoin(f.deps))
	assert.FileExists(t, f.setup.joinPendingPath(), "the marker stays while the cleanup is incomplete")
}

func TestEmptyDir(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		require.NoError(t, emptyDir(filepath.Join(t.TempDir(), "missing")))
	})

	t.Run("not a directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0600))

		require.Error(t, emptyDir(file))
	})

	t.Run("keeps the directory", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "c"), nil, 0600))

		require.NoError(t, emptyDir(dir))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("entry cannot be removed", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root removes anything")
		}

		dir := t.TempDir()
		locked := filepath.Join(dir, "locked")
		require.NoError(t, os.MkdirAll(locked, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(locked, "file"), nil, 0600))
		require.NoError(t, os.Chmod(locked, 0500))
		t.Cleanup(func() { _ = os.Chmod(locked, 0700) })

		require.Error(t, emptyDir(dir))
	})
}

func TestStopUnit_withoutSystemd(t *testing.T) {
	deps := new(mocks.BusinessDependencies)
	deps.On("SystemdUnitHandler").Return(nil, errors.New("no bus"))

	stopUnit(deps, serviceDiscoverUnit)

	deps.AssertExpectations(t)
}
