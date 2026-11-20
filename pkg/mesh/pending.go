// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"path/filepath"
	"regexp"
	"strings"
)

// scriptPrefix matches the ordering number of a pending setup script, e.g. "511-" in 511-carbonio-files.sh.
var scriptPrefix = regexp.MustCompile(`^\d+-`)

// PendingSetupService returns the definition a pending setup script belongs to, going by its name: the scripts are
// named <order>-<service>[-setup].sh and are the same in every edition of a package.
func PendingSetupService(defs []Definition, script string) (*Definition, bool) {
	name := strings.TrimSuffix(filepath.Base(script), ".sh")
	name = scriptPrefix.ReplaceAllString(name, "")

	if def, found := Find(defs, name); found {
		return def, true
	}

	return Find(defs, strings.TrimSuffix(name, "-setup"))
}
