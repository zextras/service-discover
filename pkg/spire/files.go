// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/permissions"
)

const configDirMode = 0755

// WriteFile writes content in dir/name, owned by owner with the given mode, creating dir when missing. The file is
// created with the final mode, so a secret is never readable by others even for a moment.
func WriteFile(
	deps permissions.PermissionInterface, dir, name string, content []byte, owner string, mode os.FileMode,
) (string, error) {
	err := os.MkdirAll(dir, configDirMode)
	if err != nil {
		return "", errors.WithMessagef(err, "unable to create %s", dir)
	}

	path := filepath.Join(dir, name)

	err = os.WriteFile(path, content, mode)
	if err != nil {
		return "", errors.WithMessagef(err, "unable to write %s", path)
	}

	// WriteFile does not change the mode of a pre-existing file, SetOwnership enforces it.
	return path, permissions.SetOwnership(deps, path, owner, mode)
}
