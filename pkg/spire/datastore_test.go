// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/spire"
)

const testDSN = "postgres://spire:s3cret@db.example.com:5432/spire?sslmode=verify-full"

func TestReadDatastoreDSN(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), mode))
		require.NoError(t, os.Chmod(path, mode))

		return path
	}

	dsn, err := spire.ReadDatastoreDSN("", nil)
	require.NoError(t, err)
	assert.Empty(t, dsn, "no DSN means SQLite")

	dsn, err = spire.ReadDatastoreDSN(write("ok", testDSN+"\n", 0o600), nil)
	require.NoError(t, err)
	assert.Equal(t, testDSN, dsn)

	dsn, err = spire.ReadDatastoreDSN("-", strings.NewReader("  "+testDSN+"\n"))
	require.NoError(t, err)
	assert.Equal(t, testDSN, dsn)

	for name, path := range map[string]string{
		"group readable": write("group", testDSN, 0o640),
		"world readable": write("world", testDSN, 0o604),
		"missing":        filepath.Join(dir, "missing"),
		"directory":      dir,
	} {
		_, err = spire.ReadDatastoreDSN(path, nil)
		require.Error(t, err, name)
	}

	_, err = spire.ReadDatastoreDSN("-", strings.NewReader("\n"))
	require.Error(t, err, "empty")
}

func TestValidateDatastoreDSN(t *testing.T) {
	t.Parallel()

	require.NoError(t, spire.ValidateDatastoreDSN(testDSN))
	require.NoError(t, spire.ValidateDatastoreDSN(`host=db user=spire password='p"w\d' dbname=spire`))

	for name, dsn := range map[string]string{
		"empty":         "",
		"two lines":     "host=a\nhost=b",
		"control":       "host=a\x00",
		"interpolation": "host=${HOME}",
		"template":      "password=%{x}",
	} {
		require.Error(t, spire.ValidateDatastoreDSN(dsn), name)
	}
}
