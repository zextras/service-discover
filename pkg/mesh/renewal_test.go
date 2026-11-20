// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostedDefs returns the definitions with carbonio-files hosted in another process: nothing is restarted for it, the
// process reads the token at its start and keeps it.
func hostedDefs(t *testing.T) []Definition {
	t.Helper()

	defs := loadDefs(t)
	for i := range defs {
		if defs[i].Name == "carbonio-files" {
			defs[i].Restart = nil
			defs[i].TokenEnv = nil
		}
	}

	return defs
}

// A process running with the previous token must not lose it before it has been restarted.
func TestSync_renewKeepsThePreviousTokenWhenTheUnitsDoNotRestart(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	machine.failUnit = "carbonio-files.service"

	err := Sync(machine.env, loadDefs(t), SyncOptions{Renew: true})
	require.ErrorContains(t, err, "unable to restart carbonio-files.service")

	assert.Empty(t, machine.loggedOut)
	assert.True(t, machine.tokens["secret-1"], "the units still run with the previous token")
	assert.True(t, machine.tokens["secret-2"])
}

// Nothing tells when the processes of a hosted service restart: a valid token stays.
func TestSync_renewDoesNotRotateAHostedService(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, hostedDefs(t), SyncOptions{}))

	machine.systemctl = nil

	require.NoError(t, Sync(machine.env, hostedDefs(t), SyncOptions{Renew: true}))

	assert.Equal(t, 1, machine.logins)
	assert.Empty(t, machine.loggedOut)
	assert.Empty(t, machine.restarts())

	// It is replaced as soon as it is not valid.
	delete(machine.tokens, "secret-1")

	require.NoError(t, Sync(machine.env, hostedDefs(t), SyncOptions{Renew: true}))
	assert.Equal(t, 2, machine.logins)
	assert.Empty(t, machine.loggedOut)
}

// The tokens created by the package scripts cannot be logged out, and are still in use: the login token replaces
// them in the files, they are revoked later by the cluster.
func TestSync_replacesALegacyTokenWithoutLogout(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, hostedDefs(t), SyncOptions{}))

	machine.legacy["secret-1"] = true

	require.NoError(t, Sync(machine.env, hostedDefs(t), SyncOptions{}))

	token, err := os.ReadFile(machine.tokenFile("files"))
	require.NoError(t, err)
	assert.Equal(t, "secret-2", string(token))
	assert.Empty(t, machine.loggedOut, "a token that was not created by a login has nothing to log out")
	assert.True(t, machine.tokens["secret-1"])

	// The login token is then kept.
	require.NoError(t, Sync(machine.env, hostedDefs(t), SyncOptions{}))
	assert.Equal(t, 2, machine.logins)
}

// A server that just got its leader refuses every token for a moment: that is not a reason to replace them.
func TestSync_waitsForTheACLSystemBeforeJudgingTheTokens(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	machine.slept = 0
	machine.refused = 3

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Wait: time.Minute}))

	assert.Equal(t, 3*fetchInterval, machine.slept)
	assert.Equal(t, 1, machine.logins, "the token was valid: kept")
	assert.Empty(t, machine.loggedOut)
}

// When no token is ever accepted, the wait ends with the deadline and the tokens are replaced.
func TestSync_replacesTheTokensWhenTheACLSystemNeverAcceptsThem(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	machine.refused = 1000

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Wait: 10 * time.Second}))
	assert.Equal(t, 2, machine.logins)
}

// Consul numbers the ports of the sidecars in registration order: when the agent starts with another set of services,
// every sidecar restarts, not only the ones whose definition changed.
func TestSync_restartsAllTheSidecarsWhenTheAgentLoadedAnotherServiceSet(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	defs := loadDefs(t)
	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))

	machine.systemctl = nil

	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))
	assert.Empty(t, machine.restarts(), "nothing changed")

	require.NoError(t, os.WriteFile(machine.env.sidecarsMarker(), nil, 0o600))

	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))
	assert.Contains(t, machine.restarts(), "carbonio-files-sidecar.service")
	assert.NoFileExists(t, machine.env.sidecarsMarker())

	machine.systemctl = nil

	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))
	assert.Empty(t, machine.restarts(), "the marker is consumed")
}

func TestSync_keepsTheSidecarsMarkerWhenARestartFails(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	defs := loadDefs(t)
	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))
	require.NoError(t, os.WriteFile(machine.env.sidecarsMarker(), nil, 0o600))

	machine.failUnit = "carbonio-files-sidecar.service"

	require.Error(t, Sync(machine.env, defs, SyncOptions{}))
	assert.FileExists(t, machine.env.sidecarsMarker(), "the next run restarts them again")
}

func TestConsulConfigArgs_marksAChangedServiceSet(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	defs := loadDefs(t)

	// The first start from the rendered directory: the services came from the packages before.
	_, err := ConsulConfigArgs(machine.env, defs)
	require.NoError(t, err)
	assert.FileExists(t, machine.env.sidecarsMarker())

	require.NoError(t, os.Remove(machine.env.sidecarsMarker()))

	_, err = ConsulConfigArgs(machine.env, defs)
	require.NoError(t, err)
	assert.NoFileExists(t, machine.env.sidecarsMarker(), "the same set as the previous start")

	require.NoError(t, os.WriteFile(filepath.Join(machine.env.ServicesDir, "config.json"), []byte(`{}`), 0o644))

	_, err = ConsulConfigArgs(machine.env, defs)
	require.NoError(t, err)
	assert.FileExists(t, machine.env.sidecarsMarker(), "another set")
}

func TestEnv_HasLoginToken(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	defs := loadDefs(t)

	files, found := Find(defs, "carbonio-files")
	require.True(t, found)

	assert.False(t, machine.env.HasLoginToken(files), "no token file yet")

	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))
	assert.True(t, machine.env.HasLoginToken(files))

	machine.legacy["secret-1"] = true
	assert.False(t, machine.env.HasLoginToken(files), "a token created by hand is not a login token")

	machine.legacy = map[string]bool{}
	delete(machine.tokens, "secret-1")
	assert.False(t, machine.env.HasLoginToken(files), "a token that is gone")
}
