// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/spire"
)

const (
	tarballMain = `{"node_name":"` + testNode + `","server":false,"encrypt":"gossip","acl":{"enabled":true}}`
	oldTokens   = `{"default":"old-agent-secret"}`
)

type migrateFixture struct {
	*joinFixture

	leader *string
}

// newMigrateFixture builds an agent set up with the cluster credentials.
func newMigrateFixture(t *testing.T, jwtErr error) *migrateFixture {
	t.Helper()

	f := &migrateFixture{joinFixture: newJoinFixture(t, jwtErr), leader: new(string)}
	*f.leader = "10.0.0.1:8300"

	previousLeader, previousTimeout, previousPoll := consulStatusLeader, migrateRejoinTimeout, migratePollInterval
	consulStatusLeader = func() (string, error) { return *f.leader, nil }
	migrateRejoinTimeout, migratePollInterval = 30*time.Millisecond, time.Millisecond

	t.Cleanup(func() {
		consulStatusLeader, migrateRejoinTimeout, migratePollInterval = previousLeader, previousTimeout, previousPoll
	})

	f.setup.ClusterCredential = filepath.Join(f.dirs.consul, "cluster-credential")
	f.setup.BindAddress = ""
	f.setup.Join = "-"
	f.setup.Migrate = true

	require.NoError(t, os.MkdirAll(f.setup.ConsulData, 0755))
	require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig, []byte(tarballMain), 0600))
	require.NoError(t, os.WriteFile(f.setup.MutableConfigFile, []byte(`{"bind_addr":"192.168.1.1"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(f.setup.ConsulData, aclTokensFile), []byte(oldTokens), 0600))
	require.NoError(t, os.WriteFile(f.setup.ClusterCredential, []byte("encrypted"), 0600))

	for _, name := range []string{command.ConsulCAKey, command.ConsulAgentCertificate, command.ConsulAgentCertificateKey} {
		require.NoError(t, os.WriteFile(filepath.Join(f.dirs.consul, name), []byte("old"), 0600))
	}

	require.NoError(t, os.WriteFile(filepath.Join(f.dirs.consul, command.ConsulCA), []byte("old-ca"), 0600))

	f.setup.Stdin = strings.NewReader(testBundle(t))

	return f
}

func (f *migrateFixture) assertOldSetupRestored(t *testing.T) {
	t.Helper()

	assert.Equal(t, tarballMain, string(mustReadFile(t, f.setup.ConsulFileConfig)))
	assert.Equal(t, oldTokens, string(mustReadFile(t, filepath.Join(f.setup.ConsulData, aclTokensFile))))
	assert.Equal(t, "old-ca", string(mustReadFile(t, filepath.Join(f.dirs.consul, command.ConsulCA))))
	assert.FileExists(t, f.setup.ClusterCredential)
	assert.NoFileExists(t, filepath.Join(f.dirs.spire, spire.AgentConfFile))
	assert.NoFileExists(t, filepath.Join(f.dirs.consul, introTokenFile))
	assert.NoFileExists(t, f.setup.migrateMarkerPath())
	assert.NoDirExists(t, f.setup.migrateBackupPath())
	assert.Equal(t, "127.0.0.1 localhost\n", string(mustReadFile(t, f.dirs.hosts)))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	return content
}

func TestSetup_migrate(t *testing.T) {
	f := newMigrateFixture(t, nil)

	out, err := f.setup.migrate(f.deps)
	require.NoError(t, err)

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "migrated to SPIRE")
	assert.Contains(t, plain, "still valid in Consul")

	kind, _, err := f.setup.existingConfig()
	require.NoError(t, err)
	assert.Equal(t, configJoined, kind)
	assert.NotContains(t, string(mustReadFile(t, f.setup.ConsulFileConfig)), "gossip")

	assert.Equal(t, testJWT, string(mustReadFile(t, filepath.Join(f.dirs.consul, introTokenFile))))
	assert.Equal(t, testCA, string(mustReadFile(t, filepath.Join(f.dirs.consul, command.ConsulCA))))
	assert.FileExists(t, filepath.Join(f.dirs.spire, spire.AgentConfFile))

	// what the old setup left is gone
	assert.NoFileExists(t, filepath.Join(f.setup.ConsulData, aclTokensFile))

	for _, path := range []string{
		f.setup.ClusterCredential,
		filepath.Join(f.dirs.consul, command.ConsulCAKey),
		filepath.Join(f.dirs.consul, command.ConsulAgentCertificate),
		filepath.Join(f.dirs.consul, command.ConsulAgentCertificateKey),
		f.setup.migrateMarkerPath(),
		f.setup.migrateBackupPath(),
	} {
		assert.NoFileExists(t, path)
	}

	f.unit.AssertCalled(t, "StopUnit", serviceDiscoverUnit, "replace", mock.Anything)

	// run again: nothing to do, and the bundle (already consumed) is not even read
	f.setup.Stdin = strings.NewReader("")

	out, err = f.setup.migrate(f.deps)
	require.NoError(t, err)

	plain, err = out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "nothing to migrate")
}

func TestSetup_migrate_rollsBackWhenTheAgentDoesNotRejoin(t *testing.T) {
	f := newMigrateFixture(t, nil)
	*f.leader = ""

	_, err := f.setup.migrate(f.deps)
	require.ErrorContains(t, err, "did not rejoin")
	require.ErrorContains(t, err, "previous configuration was restored")

	f.assertOldSetupRestored(t)
	assert.FileExists(t, filepath.Join(f.dirs.consul, command.ConsulCAKey), "nothing is deleted before it worked")

	// the node can be migrated afterwards
	*f.leader = "10.0.0.1:8300"
	f.setup.Stdin = strings.NewReader(testBundle(t))

	_, err = f.setup.migrate(f.deps)
	require.NoError(t, err)
}

func TestSetup_migrate_rollsBackWhenSPIREFails(t *testing.T) {
	f := newMigrateFixture(t, errors.New("join token does not exist"))

	_, err := f.setup.migrate(f.deps)
	require.ErrorContains(t, err, "valid only once")

	f.assertOldSetupRestored(t)
}

func TestSetup_migrate_refusals(t *testing.T) {
	t.Run("bundle of another node", func(t *testing.T) {
		f := newMigrateFixture(t, nil)
		require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig,
			[]byte(strings.ReplaceAll(tarballMain, testNode, "agent-other")), 0600))

		_, err := f.setup.migrate(f.deps)
		require.ErrorContains(t, err, "generate it for this node")
		assert.NoDirExists(t, f.setup.migrateBackupPath())
	})

	t.Run("server", func(t *testing.T) {
		f := newMigrateFixture(t, nil)
		require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig, []byte(`{"server":true}`), 0600))

		_, err := f.setup.migrate(f.deps)
		require.ErrorContains(t, err, "spire enable")
	})

	t.Run("not set up", func(t *testing.T) {
		f := newMigrateFixture(t, nil)
		require.NoError(t, os.Remove(f.setup.ConsulFileConfig))

		_, err := f.setup.migrate(f.deps)
		require.ErrorContains(t, err, "not set up")
	})
}

func TestSetup_migrate_afterACrash(t *testing.T) {
	f := newMigrateFixture(t, nil)

	// the previous run saved the configuration, switched it and died
	require.NoError(t, f.setup.backupForMigration())
	require.NoError(t, os.WriteFile(f.setup.ConsulFileConfig, []byte(`{"auto_config":{"enabled":true}}`), 0600))
	require.NoError(t, os.Remove(filepath.Join(f.setup.ConsulData, aclTokensFile)))
	require.NoError(t, os.WriteFile(filepath.Join(f.setup.ConsulData, autoConfigFile), []byte("{}"), 0600))

	out, err := f.setup.migrate(f.deps)
	require.NoError(t, err)

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "interrupted migration was restored")
	assert.Contains(t, plain, "migrated to SPIRE")
	assert.NoFileExists(t, filepath.Join(f.setup.ConsulData, autoConfigFile))
}
