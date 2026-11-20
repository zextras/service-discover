// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	carbonioMocks "github.com/zextras/service-discover/pkg/carbonio/mocks"
	"github.com/zextras/service-discover/pkg/spire"
)

// credCovArchive encrypts a tarball like the cluster credentials, from the entries of build, followed by the raw bytes
// trailer that are not a valid tar header when not empty.
func credCovArchive(t *testing.T, password string, build func(tw *tar.Writer), trailer []byte) []byte {
	t.Helper()

	var out bytes.Buffer

	armored, err := armor.Encode(&out, "PGP MESSAGE", nil)
	require.NoError(t, err)

	encrypted, err := openpgp.SymmetricallyEncrypt(armored, []byte(password), nil, nil)
	require.NoError(t, err)

	tw := tar.NewWriter(encrypted)
	build(tw)
	require.NoError(t, tw.Flush())

	_, err = encrypted.Write(trailer)
	require.NoError(t, err)
	require.NoError(t, encrypted.Close())
	require.NoError(t, armored.Close())

	return out.Bytes()
}

func credCovFile(t *testing.T, tw *tar.Writer, name, content string) {
	t.Helper()

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: name, Mode: 0o640, Size: int64(len(content)),
		ModTime: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}))

	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
}

func TestMemFileInfo(t *testing.T) {
	modTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	info := memFileInfo(credentialEntry{name: "opt/zextras/jwks", mode: 0o600, modTime: modTime, content: []byte("abc")})

	assert.Equal(t, "jwks", info.Name(), "only the last element of the path")
	assert.Equal(t, int64(3), info.Size())
	assert.Equal(t, fs.FileMode(0o600), info.Mode())
	assert.Equal(t, modTime, info.ModTime())
	assert.False(t, info.IsDir())
	assert.Nil(t, info.Sys())
}

func TestReadCredentialEntries(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, content []byte) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, content, 0o600))

		return path
	}

	t.Run("regular files only", func(t *testing.T) {
		archive := credCovArchive(t, "pw", func(tw *tar.Writer) {
			credCovFile(t, tw, "opt/zextras/ca.pem", "ca")
			require.NoError(t, tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "opt/", Mode: 0o755}))
			credCovFile(t, tw, spire.ClusterJWKSURLFile, "https://127.0.0.1:8082/keys")
		}, nil)

		entries, err := readCredentialEntries(write("ok", archive), "pw")
		require.NoError(t, err)
		require.Len(t, entries, 2, "the directory is skipped")
		assert.Equal(t, "opt/zextras/ca.pem", entries[0].name)
		assert.Equal(t, fs.FileMode(0o640), entries[0].mode)
		assert.Equal(t, "ca", string(entries[0].content))
		assert.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), entries[0].modTime.UTC())
		assert.Equal(t, "https://127.0.0.1:8082/keys", string(entries[1].content))
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := readCredentialEntries(filepath.Join(dir, "missing"), "pw")
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("wrong password", func(t *testing.T) {
		archive := credCovArchive(t, "pw", func(tw *tar.Writer) { credCovFile(t, tw, "a", "a") }, nil)

		_, err := readCredentialEntries(write("wrong", archive), "other")
		require.Error(t, err)
	})

	t.Run("not an archive", func(t *testing.T) {
		_, err := readCredentialEntries(write("junk", []byte("junk")), "pw")
		require.Error(t, err)
	})

	t.Run("corrupted tarball", func(t *testing.T) {
		archive := credCovArchive(t, "pw", func(tw *tar.Writer) { credCovFile(t, tw, "a", "a") },
			bytes.Repeat([]byte{'A'}, 512))

		_, err := readCredentialEntries(write("corrupted", archive), "pw")
		require.ErrorIs(t, err, tar.ErrHeader)
	})

	t.Run("truncated entry", func(t *testing.T) {
		var header bytes.Buffer

		headerWriter := tar.NewWriter(&header)
		require.NoError(t, headerWriter.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg, Name: "a", Mode: 0o600, Size: 1000,
		}))

		archive := credCovArchive(t, "pw", func(*tar.Writer) {}, append(header.Bytes(), "short"...))

		_, err := readCredentialEntries(write("truncated", archive), "pw")
		require.Error(t, err)
	})
}

func TestWriteCredentialEntries(t *testing.T) {
	t.Run("the entries can be read back and no temporary file stays", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cluster-credentials.tar.gpg")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))

		entries := []credentialEntry{
			{name: "a/b.txt", mode: 0o600, modTime: time.Now(), content: []byte("first")},
			{name: "c.txt", mode: 0o640, modTime: time.Now(), content: nil},
		}

		require.NoError(t, writeCredentialEntries(path, "pw", entries))

		read, err := readCredentialEntries(path, "pw")
		require.NoError(t, err)
		require.Len(t, read, 2)
		assert.Equal(t, "a/b.txt", read[0].name)
		assert.Equal(t, "first", string(read[0].content))
		assert.Equal(t, "c.txt", read[1].name)
		assert.Empty(t, read[1].content)

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())

		left, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, left, 1)
	})

	t.Run("the credentials are untouched when the directory is not writable", func(t *testing.T) {
		err := writeCredentialEntries(filepath.Join(t.TempDir(), "missing", "credentials"), "pw", nil)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("the destination cannot be replaced", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "credentials")
		require.NoError(t, os.MkdirAll(filepath.Join(path, "not-empty"), 0o700))

		require.Error(t, writeCredentialEntries(path, "pw", nil))

		left, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, left, 1, "the temporary file is removed")
	})
}

type publishEnv struct {
	*flowEnv
}

func newPublishEnv(t *testing.T, build func(tw *tar.Writer)) *publishEnv {
	t.Helper()

	env := newFlowEnv(t)
	env.downloaded = credCovArchive(t, "pw", build, nil)

	return &publishEnv{flowEnv: env}
}

func TestSetup_publishJWKSURL(t *testing.T) {
	const url = "https://127.0.0.1:8082/keys"

	otherFiles := func(tw *tar.Writer) { credCovFile(t, tw, "opt/zextras/ca.pem", "ca") }

	t.Run("adds the URL to the credentials without it", func(t *testing.T) {
		env := newPublishEnv(t, otherFiles)

		changed, err := env.setup.publishJWKSURL(env.ldap, "pw", url)
		require.NoError(t, err)
		assert.True(t, changed)

		local, err := readCredentialEntries(env.setup.ClusterCredential, "pw")
		require.NoError(t, err)
		require.Len(t, local, 2)
		assert.Equal(t, "opt/zextras/ca.pem", local[0].name, "the rest of the archive is kept")
		assert.Equal(t, spire.ClusterJWKSURLFile, local[1].name)
		assert.Equal(t, url, string(local[1].content))
		assert.Equal(t, fs.FileMode(0o600), local[1].mode)

		published := filepath.Join(t.TempDir(), "published")
		require.NoError(t, os.WriteFile(published, env.uploaded, 0o600))

		remote, err := readCredentialEntries(published, "pw")
		require.NoError(t, err)
		assert.Equal(t, local, remote, "LDAP gets what this server keeps")
	})

	t.Run("replaces an outdated URL", func(t *testing.T) {
		env := newPublishEnv(t, func(tw *tar.Writer) {
			credCovFile(t, tw, spire.ClusterJWKSURLFile, "https://mail.example.com:8082/keys")
			otherFiles(tw)
		})

		changed, err := env.setup.publishJWKSURL(env.ldap, "pw", url)
		require.NoError(t, err)
		assert.True(t, changed)

		local, err := readCredentialEntries(env.setup.ClusterCredential, "pw")
		require.NoError(t, err)
		require.Len(t, local, 2)
		assert.Equal(t, url, string(local[0].content))
		assert.Equal(t, "ca", string(local[1].content))
	})

	t.Run("nothing changes when LDAP already has the URL, this server gets the LDAP copy", func(t *testing.T) {
		env := newPublishEnv(t, func(tw *tar.Writer) { credCovFile(t, tw, spire.ClusterJWKSURLFile, url) })
		require.NoError(t, os.WriteFile(env.setup.ClusterCredential, []byte("stale"), 0o600))

		changed, err := env.setup.publishJWKSURL(env.ldap, "pw", url)
		require.NoError(t, err)
		assert.False(t, changed)
		env.ldap.AssertNotCalled(t, "UploadBinary", mock.Anything, mock.Anything, mock.Anything)

		content, err := os.ReadFile(env.setup.ClusterCredential)
		require.NoError(t, err)
		assert.Equal(t, env.downloaded, content)
	})

	failures := []struct {
		name    string
		prepare func(env *publishEnv)
		wantErr string
	}{
		{name: "no directory for the temporary file", wantErr: "no such file or directory",
			prepare: func(env *publishEnv) {
				env.setup.ClusterCredential = filepath.Join(t.TempDir(), "missing", "credentials")
			}},
		{name: "LDAP not available", wantErr: "ldap down", prepare: func(env *publishEnv) {
			env.ldap = new(carbonioMocks.LdapHandler)
			env.ldap.On("CheckServerAvailability", mock.Anything).Return(errors.New("ldap down"))
		}},
		{name: "wrong password", wantErr: "", prepare: func(env *publishEnv) {
			env.downloaded = credCovArchive(t, "other", otherFiles, nil)
		}},
		{name: "credentials not uploaded", wantErr: "unable to upload data to ldap", prepare: func(env *publishEnv) {
			env.ldap = new(carbonioMocks.LdapHandler)
			env.ldap.On("CheckServerAvailability", mock.Anything).Return(nil)
			env.ldap.On("DownloadBinary", mock.Anything, mock.Anything).Return(env.downloaded, nil)
			env.ldap.On("UploadBinary", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("denied"))
		}},
		{name: "local copy cannot be replaced", wantErr: "exists", prepare: func(env *publishEnv) {
			require.NoError(t, os.MkdirAll(filepath.Join(env.setup.ClusterCredential, "dir"), 0o700))
		}},
	}

	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			env := newPublishEnv(t, otherFiles)
			tt.prepare(env)

			changed, err := env.setup.publishJWKSURL(env.ldap, "pw", url)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.False(t, changed)
		})
	}
}

func TestSetup_syncLocalCredential(t *testing.T) {
	dir := t.TempDir()
	reference := filepath.Join(dir, "reference")
	require.NoError(t, os.WriteFile(reference, []byte("ldap"), 0o600))

	t.Run("a missing local copy is created", func(t *testing.T) {
		s := &Setup{ClusterCredential: filepath.Join(dir, "local-new")}

		require.NoError(t, s.syncLocalCredential(reference))

		content, err := os.ReadFile(s.ClusterCredential)
		require.NoError(t, err)
		assert.Equal(t, "ldap", string(content))
	})

	t.Run("an identical copy is not rewritten", func(t *testing.T) {
		local := filepath.Join(dir, "local-same")
		require.NoError(t, os.WriteFile(local, []byte("ldap"), 0o400))

		require.NoError(t, (&Setup{ClusterCredential: local}).syncLocalCredential(reference))

		info, err := os.Stat(local)
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o400), info.Mode().Perm(), "a rewrite would have failed or changed the mode")
	})

	t.Run("a different copy is replaced", func(t *testing.T) {
		local := filepath.Join(dir, "local-old")
		require.NoError(t, os.WriteFile(local, []byte("old"), 0o600))

		require.NoError(t, (&Setup{ClusterCredential: local}).syncLocalCredential(reference))

		content, err := os.ReadFile(local)
		require.NoError(t, err)
		assert.Equal(t, "ldap", string(content))
	})

	t.Run("missing reference", func(t *testing.T) {
		err := (&Setup{ClusterCredential: filepath.Join(dir, "x")}).syncLocalCredential(filepath.Join(dir, "none"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}
