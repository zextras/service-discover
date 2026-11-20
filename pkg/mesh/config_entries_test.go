// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"testing"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func filesEntries(t *testing.T) []api.ConfigEntry {
	t.Helper()

	defs, err := Load()
	require.NoError(t, err)

	files, found := Find(defs, "carbonio-files")
	require.True(t, found)

	entries, err := files.ConfigEntries()
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	return entries
}

func TestApply_skipsTheConfigEntriesThatAreUpToDate(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	cluster := newFakeCluster()
	options := ApplyOptions{TrustDomain: "carbonio.lab", AuthMethod: "spire"}

	first := &bytes.Buffer{}
	options.Out = first

	require.NoError(t, Apply(cluster, defs, options))
	assert.Contains(t, first.String(), "carbonio-files: config entries: 3 created, 0 updated, 0 unchanged")
	assert.Regexp(t, `config entries: \d+ created, 0 updated, 0 unchanged\n$`, first.String())

	written := map[string]api.ConfigEntry{}
	for key, entry := range cluster.entries {
		written[key] = entry
	}

	second := &bytes.Buffer{}
	options.Out = second

	require.NoError(t, Apply(cluster, defs, options))
	assert.Regexp(t, `config entries: 0 created, 0 updated, \d+ unchanged\n$`, second.String())
	assert.Contains(t, second.String(), "carbonio-files: config entries: 0 created, 0 updated, 3 unchanged")
}

func TestApply_updatesAChangedConfigEntry(t *testing.T) {
	t.Parallel()

	cluster := newFakeCluster()

	entries := filesEntries(t)

	counts, err := ensureConfigEntries(cluster, entries)
	require.NoError(t, err)
	assert.Equal(t, entryCounts{created: len(entries)}, counts)

	// Someone changed the protocol of the service by hand.
	protocol, ok := cluster.entries["service-defaults/carbonio-files"].(*api.ServiceConfigEntry)
	require.True(t, ok)

	changed := *protocol
	changed.Protocol = "tcp"
	cluster.entries["service-defaults/carbonio-files"] = &changed

	counts, err = ensureConfigEntries(cluster, entries)
	require.NoError(t, err)
	assert.Equal(t, entryCounts{updated: 1, unchanged: len(entries) - 1}, counts)
	assert.Equal(t, protocol.Protocol, cluster.entries["service-defaults/carbonio-files"].(*api.ServiceConfigEntry).Protocol)
}

// What Consul stores is the entry with its own fields: indexes, hash, the precedence of the sources, the sources in
// the order of their precedence. It is the same entry.
func TestSameConfigEntry_ignoresWhatConsulSets(t *testing.T) {
	t.Parallel()

	for _, desired := range filesEntries(t) {
		stored := desired

		intentions, ok := desired.(*api.ServiceIntentionsConfigEntry)
		if ok {
			copied := *intentions
			copied.Sources = nil

			for i := len(intentions.Sources) - 1; i >= 0; i-- {
				source := *intentions.Sources[i]
				source.Precedence = 9
				source.Type = api.IntentionSourceConsul
				copied.Sources = append(copied.Sources, &source)
			}

			copied.ModifyIndex = 12
			copied.CreateIndex = 3

			stored = &copied
		}

		assert.True(t, sameConfigEntry(desired, stored), desired.GetKind())
	}
}

func TestSameConfigEntry_noticesADifference(t *testing.T) {
	t.Parallel()

	for _, desired := range filesEntries(t) {
		intentions, ok := desired.(*api.ServiceIntentionsConfigEntry)
		if !ok {
			continue
		}

		stored := *intentions
		stored.Sources = intentions.Sources[:1]

		assert.False(t, sameConfigEntry(desired, &stored), "a source is missing")

		other := *intentions
		other.Sources = nil

		for _, source := range intentions.Sources {
			copied := *source
			copied.Permissions = nil
			other.Sources = append(other.Sources, &copied)
		}

		assert.False(t, sameConfigEntry(desired, &other), "the permissions are gone")
	}
}
