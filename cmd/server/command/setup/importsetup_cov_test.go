// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/spire"
)

const importCovDSN = "postgres://spire@db.example.com/spire"

func TestSetup_importSetup_highAvailability(t *testing.T) {
	t.Run("runs the SPIRE server and agent of this server", func(t *testing.T) {
		env := newFlowEnv(t)
		env.setup.spireDSN = importCovDSN
		env.downloaded = credentialsTarball(t, "pw", clusterFiles(t, spire.LocalJWKSURL))

		_, err := env.setup.importSetup(env.deps)
		require.NoError(t, err)

		assert.True(t, env.deps.ranCommand(spire.ServerBin+" healthcheck"), "the SPIRE server of this server")
		assert.True(t, env.deps.ranCommand(spire.ServerBin+" token generate"), "the join token of its agent")
		env.deps.unit.AssertCalled(t, "StartUnit", spire.AgentUnit, "replace", mock.Anything)

		cfg, err := os.ReadFile(env.setup.ConsulFileConfig)
		require.NoError(t, err)
		assert.Contains(t, string(cfg), spire.LocalJWKSURL)
	})

	failures := []struct {
		name    string
		prepare func(env *flowEnv)
		jwksURL string
		failOn  string
		wantErr string
	}{
		{name: "a high availability cluster needs the datastore", jwksURL: spire.LocalJWKSURL,
			wantErr: "give its connection string with --spire-datastore-file"},
		{name: "a cluster without high availability refuses the datastore", jwksURL: "https://mail.example.com:8082",
			prepare: func(env *flowEnv) { env.setup.spireDSN = importCovDSN },
			wantErr: "--spire-datastore-file cannot be used"},
		{name: "the SPIRE server does not get healthy", jwksURL: spire.LocalJWKSURL, failOn: "healthcheck",
			prepare: func(env *flowEnv) { env.setup.spireDSN = importCovDSN },
			wantErr: "unable to set up the SPIRE server"},
		{name: "the SPIRE agent cannot be set up", jwksURL: spire.LocalJWKSURL, failOn: "token generate",
			prepare: func(env *flowEnv) { env.setup.spireDSN = importCovDSN },
			wantErr: "unable to set up the SPIRE agent of this server"},
	}

	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			env := newFlowEnv(t)
			env.downloaded = credentialsTarball(t, "pw", clusterFiles(t, tt.jwksURL))
			env.failOn = tt.failOn

			if tt.prepare != nil {
				tt.prepare(env)
			}

			_, err := env.setup.importSetup(env.deps)

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSetup_importSpireAgent_skipsWithoutASPIREServer(t *testing.T) {
	env := newFlowEnv(t)

	require.NoError(t, env.setup.importSpireAgent(env.deps, flowHostname, false))
	assert.Empty(t, env.deps.commands, "no SPIRE agent without a SPIRE server on this host")
}

func TestSetup_savePasswordAndEnableService(t *testing.T) {
	t.Run("stores the password and enables the unit", func(t *testing.T) {
		env := newFlowEnv(t)

		require.NoError(t, env.setup.savePasswordAndEnableService(env.deps, false))

		content, err := os.ReadFile(filepath.Join(env.setup.ConsulHome, "password"))
		require.NoError(t, err)
		assert.Equal(t, "pw", string(content))
		env.deps.unit.AssertCalled(t, "EnableUnitFiles", mock.Anything, false, false)
	})

	t.Run("inside a container the unit is left alone", func(t *testing.T) {
		env := newFlowEnv(t)

		require.NoError(t, env.setup.savePasswordAndEnableService(env.deps, true))

		_, err := os.Stat(filepath.Join(env.setup.ConsulHome, "password"))
		require.NoError(t, err)
		env.deps.unit.AssertNotCalled(t, "EnableUnitFiles", mock.Anything, false, false)
	})

	t.Run("the password cannot be stored", func(t *testing.T) {
		env := newFlowEnv(t)
		env.setup.ConsulHome = filepath.Join(t.TempDir(), "missing")

		require.Error(t, env.setup.savePasswordAndEnableService(env.deps, true))
	})
}
