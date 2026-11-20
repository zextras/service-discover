// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package meshcmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/mesh"
)

func settling(servicesDir string, sync func([]string, bool) error) *Sync {
	return &Sync{
		Settle:      time.Millisecond,
		servicesDir: servicesDir,
		writer:      &bytes.Buffer{},
		getuid:      func() int { return rootUID },
		spireMode:   func() bool { return true },
		sync:        sync,
	}
}

func TestNew_wiresTheCommandsToTheMachine(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	svc := New(out)

	assert.Equal(t, mesh.ServicesDir, svc.Sync.servicesDir, "the settle waits for the files of the packages")
	assert.Equal(t, os.Getuid(), svc.Sync.getuid())
	assert.Equal(t, os.Getuid(), svc.PendingSetup.getuid())
	assert.Same(t, out, svc.Sync.writer)
	assert.Same(t, out, svc.PendingSetup.writer)
	assert.NotNil(t, svc.PendingSetup.run)
	assert.NotNil(t, svc.PendingSetup.exit)
	assert.NotNil(t, svc.Sync.exit)
}

func TestRunCommand(t *testing.T) {
	t.Parallel()

	require.NoError(t, runCommand("true"))
	require.Error(t, runCommand("false"))
	require.Error(t, runCommand(filepath.Join(t.TempDir(), "missing")))
}

func TestSync_runsOnceWhenTheDefinitionsDoNotChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "files.hcl"), []byte("service {}"), 0o600))

	calls := 0
	cmd := settling(dir, func(services []string, setup bool) error {
		calls++

		assert.Equal(t, []string{"carbonio-files"}, services)
		assert.False(t, setup, "the sync only updates the credentials")

		return nil
	})
	cmd.Service = []string{"carbonio-files"}

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}))
	assert.Equal(t, 1, calls)
}

func TestSync_runsAgainWhileThePackageIsStillUnpacking(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	calls := 0

	cmd := settling(dir, func([]string, bool) error {
		calls++

		// The first two runs see a file unpacked while they run.
		if calls <= 2 {
			name := filepath.Join(dir, "file"+string(rune('a'+calls))+".hcl")

			return os.WriteFile(name, []byte("service {}"), 0o600)
		}

		return nil
	})

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}))
	assert.Equal(t, 3, calls, "it stops at the first run that sees no change")
}

func TestSync_reportsTheFailureOfTheSettledRun(t *testing.T) {
	t.Parallel()

	calls := 0
	cmd := settling(t.TempDir(), func([]string, bool) error {
		calls++

		return errSync
	})

	require.ErrorIs(t, cmd.Run(&command.GlobalCommonFlags{}), errSync)
	assert.Equal(t, 1, calls, "a failure is not retried")
}

func TestSync_withoutSettleSyncsImmediately(t *testing.T) {
	t.Parallel()

	calls := 0
	cmd := settling("", func([]string, bool) error {
		calls++

		return errSync
	})
	cmd.Settle = 0

	require.ErrorIs(t, cmd.Run(&command.GlobalCommonFlags{}), errSync)
	assert.Equal(t, 1, calls)
}

func TestSync_syncServicesRejectsAnUnknownService(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := Sync{
		Service:     []string{"no-such-service"},
		TrustDomain: "carbonio",
		writer:      out,
		getuid:      func() int { return rootUID },
		spireMode:   func() bool { return true },
	}

	err := cmd.Run(&command.GlobalCommonFlags{})

	require.ErrorContains(t, err, "unknown service: no-such-service")
	assert.Empty(t, out.String(), "nothing was synchronized")
}

// The specs package reached the node before the servers: the sync ends with the status asking to run it again, which the
// unit does after a while, instead of failing.
func TestSync_aServiceTheServersHaveNotRegisteredIsRetriedLater(t *testing.T) {
	t.Parallel()

	var (
		exits []int
		out   bytes.Buffer
	)

	cmd := settling(t.TempDir(), func([]string, bool) error {
		return fmt.Errorf("%w: carbonio-new: denied", mesh.ErrPending)
	})
	cmd.Settle = 0
	cmd.writer = &out
	cmd.exit = func(code int) { exits = append(exits, code) }

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}))
	assert.Equal(t, []int{ExitPending}, exits)
	assert.Contains(t, out.String(), "Waiting for the servers to register the services of this node")
	assert.Contains(t, out.String(), "carbonio-new: denied")

	exits = nil
	cmd.sync = func([]string, bool) error { return errSync }

	require.ErrorIs(t, cmd.Run(&command.GlobalCommonFlags{}), errSync, "another failure is one")
	assert.Empty(t, exits)
}
