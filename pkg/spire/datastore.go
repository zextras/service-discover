// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	stdinMarker = "-"
	// maxDSNSize bounds what is read for a connection string.
	maxDSNSize = 64 << 10
)

var (
	errDSNEmpty       = errors.New("the SPIRE datastore connection string is empty")
	errDSNMultiline   = errors.New("the SPIRE datastore connection string must be on a single line")
	errDSNControl     = errors.New("the SPIRE datastore connection string contains control characters")
	errDSNInterpolate = errors.New("the SPIRE datastore connection string cannot contain '${' nor '%{'")
	errDSNNotRegular  = errors.New("is not a regular file")
	errDSNPermissions = errors.New("must not be accessible by group and others (chmod 600)")
	errDSNOwner       = errors.New("must be owned by the user running the command")
)

// ReadDatastoreDSN reads the PostgreSQL connection string shared by the SPIRE servers of a high availability setup,
// either from a file or, when path is "-", from stdin: it must never be given on a command line. An empty path means
// no connection string, that is a local SQLite database. A file has to be accessible only by its owner, that has to
// be the user running the command.
func ReadDatastoreDSN(path string, stdin io.Reader) (string, error) {
	if path == "" {
		return "", nil
	}

	var (
		content []byte
		err     error
	)

	if path == stdinMarker {
		content, err = io.ReadAll(io.LimitReader(stdin, maxDSNSize))
	} else {
		content, err = readPrivateFile(path)
	}

	if err != nil {
		return "", fmt.Errorf("unable to read the SPIRE datastore connection string: %w", err)
	}

	dsn := strings.TrimSpace(string(content))

	return dsn, ValidateDatastoreDSN(dsn)
}

// ValidateDatastoreDSN checks that dsn can be safely written in the HCL configuration of the SPIRE server.
func ValidateDatastoreDSN(dsn string) error {
	if dsn == "" {
		return errDSNEmpty
	}

	if strings.ContainsAny(dsn, "\r\n") {
		return errDSNMultiline
	}

	if strings.IndexFunc(dsn, unicode.IsControl) >= 0 {
		return errDSNControl
	}

	if strings.Contains(dsn, "${") || strings.Contains(dsn, "%{") {
		return errDSNInterpolate
	}

	return nil
}

func readPrivateFile(path string) ([]byte, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s %w", path, errDSNNotRegular)
	}

	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s %w", path, errDSNPermissions)
	}

	if !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("%s %w", path, errDSNOwner)
	}

	return io.ReadAll(io.LimitReader(file, maxDSNSize))
}
