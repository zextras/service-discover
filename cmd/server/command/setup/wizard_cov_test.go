// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWizard_readSpireDatastore(t *testing.T) {
	dsnFile := func(t *testing.T, content string, mode os.FileMode) string {
		t.Helper()

		path := filepath.Join(t.TempDir(), "dsn")
		require.NoError(t, os.WriteFile(path, []byte(content), mode))

		return path
	}

	tests := []struct {
		name    string
		file    func(t *testing.T) string
		wantDSN string
		wantErr string
	}{
		{name: "no datastore file means a local database", file: func(*testing.T) string { return "" }},
		{name: "stdin is reserved to the wizard", file: func(*testing.T) string { return "-" },
			wantErr: "the wizard reads its input from the terminal"},
		{name: "connection string from a file", wantDSN: "postgres://spire@db/spire", file: func(t *testing.T) string {
			return dsnFile(t, "postgres://spire@db/spire\n", 0o600)
		}},
		{name: "file accessible by others", wantErr: "chmod 600", file: func(t *testing.T) string {
			return dsnFile(t, "postgres://spire@db/spire", 0o644)
		}},
		{name: "unreadable file", wantErr: "unable to read the SPIRE datastore connection string",
			file: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wizard := NewWizardSetup(&Setup{})
			wizard.SpireDatastoreFile = tt.file(t)

			err := wizard.readSpireDatastore()

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantDSN, wizard.originalSetup.spireDSN)
		})
	}
}
