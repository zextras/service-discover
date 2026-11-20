// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package meshtest is for the tests of the packages loading the service definitions: it points the loader at the
// fixture of package mesh, a subset of the definitions of the service-discover-specs package. The installed package is
// not there when the tests run, and its checks (the owner must be root) do not apply to the directory of a build.
package meshtest

import (
	"os"
	"path/filepath"
	"runtime"
)

// UseFixture sets the specs directory of the process to the fixture. Call it from TestMain.
func UseFixture() {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("unable to locate the fixture of the service definitions")
	}

	err := os.Setenv("SERVICE_DISCOVER_SPECS_DIR", filepath.Join(filepath.Dir(file), "..", "testdata", "specs"))
	if err != nil {
		panic(err)
	}
}
