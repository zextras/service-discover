// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/mesh"
)

var errNoConsul = errors.New("connection refused")

// unattended returns the command the systemd unit runs: --unattended, the server at leader, the manifests of the
// package one after the other.
func (f *fixture) unattended(t *testing.T, leader leaderInfo, manifests ...string) (*Apply, *[]time.Duration) {
	t.Helper()

	cmd := f.command(true, 0)
	cmd.Unattended = true
	cmd.Wait = 6 * time.Second
	cmd.bootstrap = &command.BootstrapToken{}
	cmd.passwordFile = filepath.Join(t.TempDir(), "password")
	cmd.leader = func() (leaderInfo, error) { return leader, nil }

	slept := &[]time.Duration{}
	cmd.sleep = func(d time.Duration) { *slept = append(*slept, d) }

	reads := 0
	cmd.manifest = func() (*mesh.Manifest, error) {
		hash := manifests[min(reads, len(manifests)-1)]
		reads++

		return &mesh.Manifest{Name: "service-discover-specs", TreeSHA256: hash}, nil
	}

	return cmd, slept
}

func TestApply_unattendedTheLeaderAppliesTheDefinitionsAndRegistersTheIdentities(t *testing.T) {
	fix := newFixture()
	cmd, _ := fix.unattended(t, leaderInfo{Address: "10.0.0.1:8300", Local: true}, "tree")

	require.NoError(t, run(t, cmd)) // $SETUP_CONSUL_TOKEN is the bootstrap token

	assert.Equal(t, [][]string{nil}, fix.applied)
	assert.Len(t, fix.ensurer.entries, 2)
	assert.Contains(t, fix.out.String(), "identities of the services of mail.example.com registered")
}

func TestApply_unattendedANodeThatDoesNotLeadAppliesNothing(t *testing.T) {
	fix := newFixture()
	cmd, _ := fix.unattended(t, leaderInfo{Address: "10.0.0.2:8300"}, "tree")

	require.NoError(t, run(t, cmd))

	assert.Empty(t, fix.applied, "the leader applies")
	assert.Contains(t, fix.out.String(), "does not lead the cluster (the leader is 10.0.0.2:8300)")
	assert.Len(t, fix.ensurer.entries, 2, "the SPIRE entries are registered by the server running SPIRE")
}

func TestApply_unattendedANodeNotRunningSpireRegistersNoEntry(t *testing.T) {
	fix := newFixture()
	cmd, _ := fix.unattended(t, leaderInfo{Address: "10.0.0.2:8300"}, "tree")
	cmd.runsSpire = func() bool { return false }

	require.NoError(t, run(t, cmd))

	assert.Empty(t, fix.applied)
	assert.Empty(t, fix.ensurer.entries)
}

func TestApply_unattendedWithoutAStoredPasswordTheLeaderSaysSoAndSucceeds(t *testing.T) {
	fix := newFixture()
	cmd, _ := fix.unattended(t, leaderInfo{Address: "10.0.0.1:8300", Local: true}, "tree")

	t.Setenv(command.SetupConsulToken, "")

	require.NoError(t, cmd.Run(&command.GlobalCommonFlags{}))

	assert.Empty(t, fix.applied)
	assert.Contains(t, fix.out.String(), "no stored cluster password")
	assert.Contains(t, fix.out.String(), cmd.passwordFile)
	assert.Contains(t, fix.out.String(), "run 'service-discover mesh apply'")
}

func TestApply_unattendedWaitsForTheLeader(t *testing.T) {
	fix := newFixture()
	cmd, slept := fix.unattended(t, leaderInfo{Address: "10.0.0.1:8300", Local: true}, "tree")

	elections := 0
	cmd.leader = func() (leaderInfo, error) {
		elections++
		if elections < 3 {
			return leaderInfo{}, nil
		}

		return leaderInfo{Address: "10.0.0.1:8300", Local: true}, nil
	}

	require.NoError(t, run(t, cmd))

	assert.Equal(t, 3, elections)
	assert.Equal(t, [][]string{nil}, fix.applied)
	assert.Equal(t, []time.Duration{0, 2 * time.Second, 2 * time.Second}, *slept, "the settle, then the two polls")

	cmd, _ = fix.unattended(t, leaderInfo{}, "tree")
	require.ErrorIs(t, run(t, cmd), errNoLeader, "a cluster that never has a leader")

	cmd, _ = fix.unattended(t, leaderInfo{}, "tree")
	cmd.leader = func() (leaderInfo, error) { return leaderInfo{}, errNoConsul }
	require.ErrorIs(t, run(t, cmd), errNoConsul)
}

func TestApply_unattendedWaitsForThePackageToBeComplete(t *testing.T) {
	fix := newFixture()
	cmd, slept := fix.unattended(t, leaderInfo{Address: "10.0.0.1:8300", Local: true}, "tree")

	loads := 0
	cmd.definitions = func() ([]mesh.Definition, error) {
		loads++
		if loads < 3 {
			return nil, mesh.ErrSpecsUntrusted
		}

		return []mesh.Definition{{Name: "carbonio-files", User: "carbonio-files"}}, nil
	}

	require.NoError(t, run(t, cmd))

	assert.Equal(t, [][]string{nil}, fix.applied)
	assert.Equal(t, []time.Duration{0, time.Second, time.Second}, (*slept)[:3])

	cmd, _ = fix.unattended(t, leaderInfo{Address: "10.0.0.1:8300", Local: true}, "tree")
	cmd.definitions = func() ([]mesh.Definition, error) { return nil, mesh.ErrSpecsMissing }

	require.ErrorIs(t, run(t, cmd), mesh.ErrSpecsMissing, "a package that never becomes complete")
	assert.Len(t, fix.applied, 1, "nothing is applied from it")
}

func TestApply_unattendedRunsAgainWhenThePackageChangedDuringTheRun(t *testing.T) {
	fix := newFixture()
	// read before the first run, after it (changed), before the second run, after it
	cmd, _ := fix.unattended(t, leaderInfo{Address: "10.0.0.1:8300", Local: true}, "tree-1", "tree-2", "tree-2", "tree-2")

	require.NoError(t, run(t, cmd))

	assert.Len(t, fix.applied, 2)
}

func TestApply_unattendedErrors(t *testing.T) {
	fix := newFixture()
	cmd, _ := fix.unattended(t, leaderInfo{Address: "10.0.0.1:8300", Local: true}, "tree")
	cmd.RevokeLegacy = true

	require.ErrorIs(t, run(t, cmd), errUnattendedRevoke)

	cmd, _ = fix.unattended(t, leaderInfo{Address: "10.0.0.1:8300", Local: true}, "tree")
	fix.applyErr = errApply

	require.ErrorIs(t, run(t, cmd), errApply)

	notRoot := fix.command(true, 1000)
	notRoot.Unattended = true

	require.ErrorIs(t, run(t, notRoot), errNotRoot)
}

func TestRaftLeader(t *testing.T) { //nolint:paralleltest // changes the environment
	tests := []struct {
		name, body string
		want       leaderInfo
	}{
		{"this server", `"127.0.0.1:8300"`, leaderInfo{Address: "127.0.0.1:8300", Local: true}},
		{"another server", `"203.0.113.7:8300"`, leaderInfo{Address: "203.0.113.7:8300"}},
		{"no leader yet", `""`, leaderInfo{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/status/leader", r.URL.Path)
				assert.Empty(t, r.Header.Get("X-Consul-Token"), "the question needs no token")

				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			t.Setenv("CONSUL_HTTP_ADDR", server.Listener.Addr().String())
			t.Setenv("CONSUL_HTTP_TOKEN", "")

			got, err := raftLeader()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Setenv("CONSUL_HTTP_ADDR", "127.0.0.1:1")

	_, err := raftLeader()
	require.ErrorContains(t, err, "unable to ask Consul for its leader")
}

func TestApply_registersTheIdentitiesOfTheOtherNodesToo(t *testing.T) {
	fix := newFixture()
	cmd := fix.command(true, 0, "carbonio-files")
	cmd.bootstrap = &command.BootstrapToken{}
	cmd.nodes = func() ([]string, error) { return []string{"agent.example.com", "mail.example.com"}, nil }

	require.NoError(t, run(t, cmd))

	assert.Equal(t, []ensured{
		{"spiffe://carbonio/node/mail.example.com", "spiffe://carbonio/service/carbonio-files", "unix:user:carbonio-files"},
		{"spiffe://carbonio/node/agent.example.com", "spiffe://carbonio/service/carbonio-files", "unix:user:carbonio-files"},
	}, fix.ensurer.entries, "this server once, the other nodes after it")
	assert.Contains(t, fix.out.String(), "of the services of mail.example.com, agent.example.com registered")

	cmd = fix.command(true, 0)
	cmd.bootstrap = &command.BootstrapToken{}
	cmd.nodes = func() ([]string, error) { return nil, errNoConsul }

	require.ErrorIs(t, run(t, cmd), errNoConsul)
}
