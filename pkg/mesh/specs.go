// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
)

const (
	// DefaultSpecsDir is where the service-discover-specs package installs the service definitions. They are read from
	// here and from nowhere else: the package is the only source of the definitions, so that what they say (ACL rules,
	// commands run as root) can be changed only by whoever can install a package.
	DefaultSpecsDir = "/usr/share/service-discover/specs"

	// SpecsDirEnv overrides DefaultSpecsDir. It is meant for the tests and for trying a definition before releasing it:
	// the directory is then read as it is, without the checks of the installed package (owner, manifest). The
	// environment of a process is not writable by anyone but the one starting it, root in the systemd units.
	SpecsDirEnv = "SERVICE_DISCOVER_SPECS_DIR"

	// ManifestFile is the file of the package telling the version of the definitions and the hash of the tree. A package
	// installing a new version of the definitions changes it, and the path units of service-discover watch it.
	ManifestFile = "manifest.json"

	rootUID       = uint32(0)
	writableOther = 0o022
)

var (
	// ErrSpecsMissing is returned when the service definitions are not installed, or a file of them is missing.
	ErrSpecsMissing = errors.New("the service definitions are missing or incomplete")
	// ErrSpecsUntrusted is returned when the service definitions on the disk are not the ones of the package.
	ErrSpecsUntrusted = errors.New("the service definitions are not the ones of the package")

	errNotRegular = errors.New("not a regular file or directory")
	errNotRoot    = errors.New("not owned by root")
	errWritable   = errors.New("writable by others than its owner")
	errManifest   = errors.New("the manifest does not match the files")
)

// Manifest is the content of manifest.json, written by the package when it is built.
type Manifest struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Services int    `json:"services"`
	// TreeSHA256 is the SHA-256 of the "sha256sum" listing of every file but the manifest, sorted by path.
	TreeSHA256 string `json:"tree_sha256"`
}

// SpecsDir returns the directory the definitions are read from, and whether it is the one of the installed package,
// that has to pass the checks of Verify.
func SpecsDir() (string, bool) {
	override := os.Getenv(SpecsDirEnv)
	if override != "" {
		return override, false
	}

	return DefaultSpecsDir, true
}

// Load returns the definitions of the services, sorted by name, read from the directory of the service-discover-specs
// package. The installed package is verified first: it must be owned by root, and match its manifest. A missing or
// incomplete tree is an ErrSpecsMissing, a tampered one an ErrSpecsUntrusted: nothing is applied from it.
func Load() ([]Definition, error) {
	dir, installed := SpecsDir()
	if installed {
		_, err := Verify(dir)
		if err != nil {
			return nil, err
		}
	}

	return LoadDir(dir)
}

// LoadDir returns the definitions of the directory, without the checks of the installed package.
func LoadDir(dir string) ([]Definition, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, missing(dir, err)
	}

	defs, err := LoadFS(os.DirFS(dir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, missing(dir, err)
		}

		return nil, fmt.Errorf("the service definitions in %s are not valid: %w", dir, err)
	}

	return defs, nil
}

func missing(dir string, cause error) error {
	if cause == nil {
		cause = errors.New("not a directory") //nolint:err113 // only a message
	}

	return fmt.Errorf("%w in %s (%w): install or reinstall the service-discover-specs package, or set %s", ErrSpecsMissing,
		dir, cause, SpecsDirEnv)
}

// ReadManifest returns the manifest of the definitions in dir.
func ReadManifest(dir string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, missing(dir, err)
	}

	manifest := &Manifest{}

	err = json.Unmarshal(raw, manifest)
	if err != nil {
		return nil, fmt.Errorf("%w: unable to decode %s: %w", ErrSpecsUntrusted, filepath.Join(dir, ManifestFile), err)
	}

	return manifest, nil
}

// Verify checks that dir is what the package installed: every directory and file is owned by root and writable by
// nobody else, there are no links, and the tree matches the hash of the manifest. A process that can write in the
// directory of a service, or a partial installation, is not trusted to say what the mesh registers. It returns the
// manifest.
func Verify(dir string) (*Manifest, error) {
	return verifyOwnedBy(dir, rootUID)
}

func verifyOwnedBy(dir string, owner uint32) (*Manifest, error) {
	manifest, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}

	hash, err := treeHash(dir, owner)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, missing(dir, err)
		}

		return nil, fmt.Errorf("%w: %w", ErrSpecsUntrusted, err)
	}

	if hash != manifest.TreeSHA256 {
		return nil, fmt.Errorf("%w in %s: %w (%s, expected %s): the package is being installed, or its files have been "+
			"changed: reinstall service-discover-specs", ErrSpecsUntrusted, dir, errManifest, hash, manifest.TreeSHA256)
	}

	return manifest, nil
}

// treeHash computes the hash of a tree as the manifest defines it, checking the owner and the mode of what it reads.
func treeHash(dir string, owner uint32) (string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, err := os.Lstat(name)
		if err != nil {
			return err
		}

		err = checkTrusted(name, info, owner)
		if err != nil {
			return err
		}

		// Like the manifest generation, whose find excludes every file named manifest.json.
		if info.Mode().IsRegular() && entry.Name() != ManifestFile {
			rel, relErr := filepath.Rel(dir, name)
			if relErr != nil {
				return relErr
			}

			files = append(files, filepath.ToSlash(rel))
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	slices.Sort(files)

	var listing bytes.Buffer

	for _, rel := range files {
		content, readErr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if readErr != nil {
			return "", readErr
		}

		sum := sha256.Sum256(content)

		listing.WriteString(hex.EncodeToString(sum[:]) + "  " + rel + "\n")
	}

	total := sha256.Sum256(listing.Bytes())

	return hex.EncodeToString(total[:]), nil
}

// checkTrusted fails for what is not a plain file or directory of the owner (root) that others cannot write.
func checkTrusted(name string, info fs.FileInfo, owner uint32) error {
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("%s: %w", name, errNotRegular)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != owner {
		return fmt.Errorf("%s: %w", name, errNotRoot)
	}

	if info.Mode().Perm()&writableOther != 0 {
		return fmt.Errorf("%s: %w", name, errWritable)
	}

	return nil
}
