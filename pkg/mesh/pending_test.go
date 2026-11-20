// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingSetupService(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	for script, want := range map[string]string{
		"511-carbonio-files.sh":                               "carbonio-files",
		"/etc/zextras/pending-setups.d/511-carbonio-files.sh": "carbonio-files",
		"carbonio-files":                                      "carbonio-files",
		"720-carbonio-message-broker.sh":                      "carbonio-message-broker",
		"carbonio-docs-editor-setup":                          "carbonio-docs-editor",
	} {
		def, found := PendingSetupService(defs, script)
		require.True(t, found, script)
		assert.Equal(t, want, def.Name, script)
	}

	_, found := PendingSetupService(defs, "100-something-else.sh")
	assert.False(t, found)
}

func TestPendingSetupClassification(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	keep := map[string]bool{}

	for _, def := range defs {
		if def.PendingSetup == PendingSetupKeep {
			keep[def.Name] = true
		}

		// What the old script did besides the mesh part must be listed, or be left to the script.
		if def.PendingSetup == PendingSetupKeep {
			assert.Empty(t, def.PostSetup, def.Name)
		}
	}

	assert.False(t, keep["carbonio-docs-editor"], "its mesh part is a setup step")
	assert.False(t, keep["carbonio-files"])
}
