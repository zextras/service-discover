// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/pkg/spire"
)

// maxClusterCredentialEntry bounds the size of a file read from the cluster credentials.
const maxClusterCredentialEntry = 1 << 20

type credentialEntry struct {
	name    string
	mode    fs.FileMode
	modTime time.Time
	content []byte
}

// memFileInfo describes an entry of the cluster credentials for the encrypter.
type memFileInfo credentialEntry

func (m memFileInfo) Name() string       { return filepath.Base(m.name) }
func (m memFileInfo) Size() int64        { return int64(len(m.content)) }
func (m memFileInfo) Mode() fs.FileMode  { return m.mode }
func (m memFileInfo) ModTime() time.Time { return m.modTime }
func (m memFileInfo) IsDir() bool        { return false }
func (m memFileInfo) Sys() any           { return nil }

func readCredentialEntries(path, password string) ([]credentialEntry, error) {
	file, err := os.Open(path) // #nosec
	if err != nil {
		return nil, err
	}

	defer file.Close()

	reader, err := encrypter.NewReader(file, []byte(password))
	if err != nil {
		return nil, err
	}

	var entries []credentialEntry

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}

		if err != nil {
			return nil, err
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		content, err := io.ReadAll(io.LimitReader(reader, maxClusterCredentialEntry))
		if err != nil {
			return nil, err
		}

		entries = append(entries, credentialEntry{
			name: header.Name, mode: fs.FileMode(header.Mode & 0777),
			modTime: header.ModTime, content: content,
		})
	}
}

// writeCredentialEntries replaces the cluster credentials with a new archive of the entries: readers never see a
// partial file.
func writeCredentialEntries(path, password string, entries []credentialEntry) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cluster-credential-")
	if err != nil {
		return err
	}

	defer os.Remove(tmp.Name())

	err = tmp.Chmod(os.FileMode(0600))
	if err != nil {
		tmp.Close()

		return err
	}

	writer, err := encrypter.NewWriter(tmp, []byte(password))
	if err != nil {
		tmp.Close()

		return err
	}

	for _, entry := range entries {
		err = writer.AddFile(bytes.NewReader(entry.content), memFileInfo(entry), entry.name, "/")
		if err != nil {
			tmp.Close()

			return err
		}
	}

	err = writer.Flush()
	if err == nil {
		err = writer.Close()
	}

	if err != nil {
		tmp.Close()

		return err
	}

	err = tmp.Close()
	if err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}

// publishJWKSURL makes the cluster credentials, the one in LDAP that the new servers download and the copy of this
// server, carry the URL where Consul fetches the SPIRE keys from, so that the servers added later run SPIRE the same
// way as the others. The rest of the archive is kept as it is, and readers that do not know about the new entry
// ignore it. It tells whether something had to be changed: the LDAP copy is the reference.
func (s *Setup) publishJWKSURL(
	ldapHandler carbonio.LdapHandler, password, jwksURL string,
) (bool, error) {
	reference, err := os.CreateTemp(filepath.Dir(s.ClusterCredential), ".cluster-credential-ldap-")
	if err != nil {
		return false, err
	}

	reference.Close()

	defer os.Remove(reference.Name())

	err = command.DownloadCredentialsFromLDAP(ldapHandler, reference.Name())
	if err != nil {
		return false, err
	}

	entries, err := readCredentialEntries(reference.Name(), password)
	if err != nil {
		return false, err
	}

	found := false

	for index, entry := range entries {
		if entry.name != spire.ClusterJWKSURLFile {
			continue
		}

		found = true

		if string(entry.content) == jwksURL {
			return false, s.syncLocalCredential(reference.Name())
		}

		entries[index].content = []byte(jwksURL)
		entries[index].modTime = time.Now()
	}

	if !found {
		entries = append(entries, credentialEntry{
			name: spire.ClusterJWKSURLFile, mode: 0600, modTime: time.Now(), content: []byte(jwksURL),
		})
	}

	err = writeCredentialEntries(s.ClusterCredential, password, entries)
	if err != nil {
		return false, err
	}

	err = command.UploadCredentialsToLDAP(ldapHandler, s.ClusterCredential)
	if err != nil {
		return false, err
	}

	return true, nil
}

// syncLocalCredential gives this server the copy of the cluster credentials of LDAP.
func (s *Setup) syncLocalCredential(reference string) error {
	content, err := os.ReadFile(reference) // #nosec
	if err != nil {
		return err
	}

	current, err := os.ReadFile(s.ClusterCredential) // #nosec
	if err == nil && bytes.Equal(current, content) {
		return nil
	}

	return os.WriteFile(s.ClusterCredential, content, os.FileMode(0600)) //nolint:gosec // configured path
}
