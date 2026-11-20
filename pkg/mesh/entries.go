// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"fmt"
)

// EntryEnsurer creates a SPIRE registration entry unless it already exists, see spire.ServerCLI.
type EntryEnsurer interface {
	EnsureEntry(parentID, spiffeID string, selectors ...string) (bool, error)
}

// NodeSpiffeID is the SPIFFE ID of the SPIRE agent running on host.
func NodeSpiffeID(trustDomain, host string) string {
	return "spiffe://" + trustDomain + "/node/" + host
}

// Entry is a SPIRE registration entry: the processes matching the selector on the node identified by the parent get
// the SPIFFE ID.
type Entry struct {
	ParentID string
	SpiffeID string
	Selector string
}

// Entries returns the registration entries of every definition for the node running on host. The parent is the
// node: only the SPIRE agent of that host can issue the identities, and only to the processes of the service user.
func Entries(defs []Definition, trustDomain, host string) []Entry {
	entries := make([]Entry, 0, len(defs))

	for i := range defs {
		entries = append(entries, Entry{
			ParentID: NodeSpiffeID(trustDomain, host),
			SpiffeID: defs[i].SpiffeID(trustDomain),
			Selector: defs[i].UnixSelector(),
		})
	}

	return entries
}

// EnsureEntries registers the identities of all the services for the node running on host, and returns how many
// entries have been created. It is idempotent. The entries do not depend on the services being installed on the node:
// a package installed later finds its identity ready.
func EnsureEntries(ensurer EntryEnsurer, defs []Definition, trustDomain, host string) (int, error) {
	created := 0

	for _, entry := range Entries(defs, trustDomain, host) {
		isNew, err := ensurer.EnsureEntry(entry.ParentID, entry.SpiffeID, entry.Selector)
		if err != nil {
			return created, fmt.Errorf("unable to register %s: %w", entry.SpiffeID, err)
		}

		if isNew {
			created++
		}
	}

	return created, nil
}
