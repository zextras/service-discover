// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEnsurer struct {
	existing map[string]bool
	calls    []Entry
	failOn   string
}

var errEnsure = errors.New("spire is down")

func (f *fakeEnsurer) EnsureEntry(parentID, spiffeID string, selectors ...string) (bool, error) {
	if spiffeID == f.failOn {
		return false, errEnsure
	}

	f.calls = append(f.calls, Entry{ParentID: parentID, SpiffeID: spiffeID, Selector: selectors[0]})

	key := parentID + spiffeID
	if f.existing[key] {
		return false, nil
	}

	f.existing[key] = true

	return true, nil
}

func TestEntries_oneEntryPerServiceUnderTheNode(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	entries := Entries(defs, "carbonio", "mail2.example.com")
	require.Len(t, entries, len(defs))

	files, _ := Find(defs, "carbonio-files")

	assert.Contains(t, entries, Entry{
		ParentID: "spiffe://carbonio/node/mail2.example.com",
		SpiffeID: "spiffe://carbonio/service/carbonio-files",
		Selector: "unix:user:" + files.User,
	})
}

func TestEnsureEntries_isIdempotent(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	ensurer := &fakeEnsurer{existing: map[string]bool{}}

	created, err := EnsureEntries(ensurer, defs, "carbonio", "mail.example.com")
	require.NoError(t, err)
	assert.Len(t, defs, created)

	created, err = EnsureEntries(ensurer, defs, "carbonio", "mail.example.com")
	require.NoError(t, err)
	assert.Zero(t, created)

	created, err = EnsureEntries(ensurer, defs, "carbonio", "mail3.example.com")
	require.NoError(t, err)
	assert.Len(t, defs, created, "every node has its own entries")
}

func TestEnsureEntries_reportsTheFailingService(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	ensurer := &fakeEnsurer{existing: map[string]bool{}, failOn: "spiffe://carbonio/service/carbonio-files"}

	_, err = EnsureEntries(ensurer, defs, "carbonio", "mail.example.com")
	require.ErrorIs(t, err, errEnsure)
	require.ErrorContains(t, err, "carbonio-files")
}
