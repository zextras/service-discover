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
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/formatter"
)

// covStdin replaces the standard input of the process with a file holding content, for the time of the test.
func covStdin(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	input, err := os.Open(path)
	require.NoError(t, err)

	previous := os.Stdin
	os.Stdin = input

	t.Cleanup(func() {
		os.Stdin = previous

		input.Close()
	})
}

func covDSNFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "dsn")
	require.NoError(t, os.WriteFile(path, []byte(content), mode))

	return path
}

func covFlags() *command.GlobalCommonFlags {
	return &command.GlobalCommonFlags{Format: formatter.PlainFormatOutput}
}

// The commands run with the real dependencies: what they have to do before reaching the system is checked here,
// whatever the machine running the test has installed, so the last step is only required to fail.
func TestSetup_Run_beforeTheSetup(t *testing.T) {
	validDSN := enableTestDSN + "\n"

	tests := []struct {
		name    string
		file    func(t *testing.T) string
		stdin   string
		message string
	}{
		{
			"the datastore file is readable by others",
			func(t *testing.T) string { return covDSNFile(t, validDSN, 0644) },
			"",
			"unable to read the SPIRE datastore connection string",
		},
		{
			"the datastore on stdin is not valid",
			func(*testing.T) string { return "-" },
			"not a dsn\n",
			"",
		},
		{
			"the datastore on stdin is read, the machine is not ready",
			func(*testing.T) string { return "-" },
			validDSN,
			"",
		},
		{
			"the datastore file is read, the machine is not ready",
			func(t *testing.T) string { return covDSNFile(t, validDSN, 0600) },
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			covStdin(t, tt.stdin)

			setup := Setup{SpireDatastoreFile: tt.file(t), Password: "pw", BindAddress: "10.0.0.2"}

			err := setup.Run(covFlags())
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.message)
		})
	}
}

func TestEnable_Run_beforeTheEnabling(t *testing.T) {
	validDSN := enableTestDSN + "\n"

	t.Run("invalid datastore", func(t *testing.T) {
		covStdin(t, "")

		enable := Enable{Password: "pw", SpireDatastoreFile: covDSNFile(t, validDSN, 0644)}

		require.ErrorContains(t, enable.Run(covFlags()), "unable to read the SPIRE datastore connection string")
	})

	t.Run("stdin carries the datastore: the password cannot be asked", func(t *testing.T) {
		covStdin(t, validDSN)

		enable := Enable{SpireDatastoreFile: "-"}

		require.ErrorContains(t, enable.Run(covFlags()), "give the password with --password")
	})

	t.Run("the password is asked and the input is over", func(t *testing.T) {
		covStdin(t, "")

		enable := Enable{}

		require.Error(t, enable.Run(covFlags()))
	})

	t.Run("password from the input, the machine is not ready", func(t *testing.T) {
		covStdin(t, "pw\n")

		enable := Enable{}

		require.Error(t, enable.Run(covFlags()))
	})

	t.Run("password given, the machine is not ready", func(t *testing.T) {
		covStdin(t, validDSN)

		enable := Enable{Password: "pw", SpireDatastoreFile: "-"}

		require.Error(t, enable.Run(covFlags()))
	})
}

func TestEnable_askPassword(t *testing.T) {
	covStdin(t, "secret\n")

	enable := Enable{}

	password, err := enable.askPassword()
	require.NoError(t, err)
	assert.Equal(t, "secret", password)
}
