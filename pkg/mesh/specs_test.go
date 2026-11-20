// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// knownTreeHash is the hash hack/manifest.sh of service-discover-specs computes for the tree written by writeTree:
//
//	(cd DIR && find . -type f ! -name manifest.json -printf '%P\n' | LC_ALL=C sort | xargs -d '\n' sha256sum) | sha256sum
const knownTreeHash = "4cb53b355915ff0760ca25a3f31187f434f8ad4a1f833cfa3731bf9a020e2fef"

func writeTree(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(dir, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), []byte("one\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "policies.json"), []byte("two\n"), 0o644))

	return dir
}

func writeManifest(t *testing.T, dir, hash string) {
	t.Helper()

	manifest := `{"name":"service-discover-specs","version":"1.2.3-1","services":1,"tree_sha256":"` + hash + `"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ManifestFile), []byte(manifest), 0o644))
}

func TestSpecsDir(t *testing.T) { //nolint:paralleltest // changes the environment
	t.Setenv(SpecsDirEnv, "")

	dir, installed := SpecsDir()
	assert.Equal(t, DefaultSpecsDir, dir)
	assert.True(t, installed, "the installed package is verified")

	t.Setenv(SpecsDirEnv, "/somewhere")

	dir, installed = SpecsDir()
	assert.Equal(t, "/somewhere", dir)
	assert.False(t, installed, "an override is read as it is")
}

func TestLoadDir_saysWhatIsMissing(t *testing.T) {
	t.Parallel()

	_, err := LoadDir(filepath.Join(t.TempDir(), "absent"))
	require.ErrorIs(t, err, ErrSpecsMissing)
	assert.ErrorContains(t, err, "install or reinstall the service-discover-specs package")
	assert.ErrorContains(t, err, SpecsDirEnv)

	empty := t.TempDir()

	_, err = LoadDir(empty)
	require.ErrorIs(t, err, ErrSpecsMissing, "no index.json")
	assert.ErrorContains(t, err, "index.json")

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err = LoadDir(file)
	require.ErrorIs(t, err, ErrSpecsMissing, "not a directory")
}

func TestLoadDir_aRemovedFileOfADefinitionIsReportedByName(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "specs")
	require.NoError(t, os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "specs"))))

	defs, err := LoadDir(dir)
	require.NoError(t, err)
	require.NotEmpty(t, defs)

	require.NoError(t, os.Remove(filepath.Join(dir, "carbonio-files", "intentions.json")))

	_, err = LoadDir(dir)
	require.ErrorIs(t, err, ErrSpecsMissing)
	assert.ErrorContains(t, err, `"carbonio-files"`)
	assert.ErrorContains(t, err, "intentions.json")
	assert.ErrorContains(t, err, "reinstall the service-discover-specs package")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "carbonio-files", "intentions.json"), []byte("{"), 0o600))

	_, err = LoadDir(dir)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrSpecsMissing, "a broken file is not a missing one")
	assert.ErrorContains(t, err, "are not valid")
}

func TestVerify_acceptsTheTreeOfTheManifest(t *testing.T) {
	t.Parallel()

	dir := writeTree(t)
	writeManifest(t, dir, knownTreeHash)

	manifest, err := verifyOwnedBy(dir, uint32(os.Getuid())) //nolint:gosec // a uid
	require.NoError(t, err)
	assert.Equal(t, "1.2.3-1", manifest.Version)
	assert.Equal(t, 1, manifest.Services)
}

func TestVerify_refusesATreeThatIsNotThePackage(t *testing.T) {
	t.Parallel()

	owner := uint32(os.Getuid()) //nolint:gosec // a uid

	t.Run("no manifest", func(t *testing.T) {
		t.Parallel()

		_, err := verifyOwnedBy(writeTree(t), owner)
		require.ErrorIs(t, err, ErrSpecsMissing)
	})

	t.Run("a manifest that is not JSON", func(t *testing.T) {
		t.Parallel()

		dir := writeTree(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, ManifestFile), []byte("{"), 0o644))

		_, err := verifyOwnedBy(dir, owner)
		require.ErrorIs(t, err, ErrSpecsUntrusted)
	})

	t.Run("a file changed after the installation", func(t *testing.T) {
		t.Parallel()

		dir := writeTree(t)
		writeManifest(t, dir, knownTreeHash)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "policies.json"), []byte("changed\n"), 0o644))

		_, err := verifyOwnedBy(dir, owner)
		require.ErrorIs(t, err, ErrSpecsUntrusted)
		assert.ErrorContains(t, err, "reinstall service-discover-specs")
	})

	t.Run("a file added", func(t *testing.T) {
		t.Parallel()

		dir := writeTree(t)
		writeManifest(t, dir, knownTreeHash)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "extra.json"), []byte("{}"), 0o644))

		_, err := verifyOwnedBy(dir, owner)
		require.ErrorIs(t, err, ErrSpecsUntrusted)
	})

	t.Run("a file others can write", func(t *testing.T) {
		t.Parallel()

		dir := writeTree(t)
		writeManifest(t, dir, knownTreeHash)
		require.NoError(t, os.Chmod(filepath.Join(dir, "a", "policies.json"), 0o666))

		_, err := verifyOwnedBy(dir, owner)
		require.ErrorIs(t, err, ErrSpecsUntrusted)
		assert.ErrorContains(t, err, "writable by others")
	})

	t.Run("a link", func(t *testing.T) {
		t.Parallel()

		dir := writeTree(t)
		writeManifest(t, dir, knownTreeHash)
		require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(dir, "a", "link.json")))

		_, err := verifyOwnedBy(dir, owner)
		require.ErrorIs(t, err, ErrSpecsUntrusted)
		assert.ErrorContains(t, err, "not a regular file")
	})

	t.Run("owned by somebody else", func(t *testing.T) {
		t.Parallel()

		dir := writeTree(t)
		writeManifest(t, dir, knownTreeHash)

		_, err := Verify(dir)
		if os.Getuid() == 0 {
			require.NoError(t, err)

			return
		}

		require.ErrorIs(t, err, ErrSpecsUntrusted)
		assert.ErrorContains(t, err, "not owned by root")
	})
}

func TestLoad_readsTheOverrideWithoutTheChecksOfThePackage(t *testing.T) { //nolint:paralleltest // changes the environment
	t.Setenv(SpecsDirEnv, filepath.Join("testdata", "specs"))

	defs, err := Load()
	require.NoError(t, err)
	assert.NotEmpty(t, defs)

	t.Setenv(SpecsDirEnv, filepath.Join(t.TempDir(), "absent"))

	_, err = Load()
	require.ErrorIs(t, err, ErrSpecsMissing)
}
