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
	"github.com/zextras/service-discover/pkg/spire"
)

func newRefreshFixture(t *testing.T, serverAddress string, peers []string, answering ...string) *joinFixture {
	t.Helper()

	f := newJoinFixture(t, nil)

	require.NoError(t, os.MkdirAll(f.dirs.spire, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(f.dirs.spire, spire.AgentConfFile),
		[]byte(spire.AgentConf(f.dirs.spire, serverAddress, 8081, "token")), 0600))
	require.NoError(t, writeHostsBlock(f.dirs.hosts, spireServersAlias, []string{"10.0.0.1", "10.0.0.9"}))

	previous := consulStatusPeers
	consulStatusPeers = func() ([]string, error) { return peers, nil }

	t.Cleanup(func() { consulStatusPeers = previous })

	dialSpireServer = func(address string) error {
		for _, ip := range answering {
			if address == ip+":8081" {
				return nil
			}
		}

		return errors.New("connection refused")
	}

	return f
}

func TestSetup_refreshSpireServers(t *testing.T) {
	f := newRefreshFixture(t, spireServersAlias, []string{"10.0.0.1:8300", "10.0.0.2:8300", "10.0.0.3:8300"},
		"10.0.0.1", "10.0.0.2")

	out, err := f.setup.refreshSpireServers(f.deps)
	require.NoError(t, err)

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "10.0.0.1, 10.0.0.2")

	want := "127.0.0.1 localhost\n" + hostsBegin + "\n10.0.0.1 " + spireServersAlias + "\n10.0.0.2 " +
		spireServersAlias + "\n" + hostsEnd + "\n"
	assert.Equal(t, want, string(mustReadFile(t, f.dirs.hosts)), "only the managed block changes")

	_, err = f.setup.refreshSpireServers(f.deps)
	require.NoError(t, err)
	assert.Equal(t, want, string(mustReadFile(t, f.dirs.hosts)))
}

func TestSetup_refreshSpireServers_nothingToDo(t *testing.T) {
	t.Run("agent connected to a single server", func(t *testing.T) {
		f := newRefreshFixture(t, "mail.example.com", []string{"10.0.0.1:8300"}, "10.0.0.1")
		before := mustReadFile(t, f.dirs.hosts)

		out, err := f.setup.refreshSpireServers(f.deps)
		require.NoError(t, err)

		plain, err := out.PlainRender()
		require.NoError(t, err)
		assert.Contains(t, plain, "nothing to refresh")
		assert.Equal(t, before, mustReadFile(t, f.dirs.hosts))
	})

	t.Run("no server answers: the hosts file is kept", func(t *testing.T) {
		f := newRefreshFixture(t, spireServersAlias, []string{"10.0.0.1:8300"})
		before := mustReadFile(t, f.dirs.hosts)

		_, err := f.setup.refreshSpireServers(f.deps)
		require.ErrorContains(t, err, "no Consul server answers")
		assert.Equal(t, before, mustReadFile(t, f.dirs.hosts))
	})
}
