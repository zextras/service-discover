// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"os"
	"path/filepath"
	"testing"
)

// The tests read the definitions of a fixture: the installed package is not there, and its checks (owner root) do
// not apply to a directory of the build.
func TestMain(m *testing.M) {
	fixture, err := filepath.Abs(filepath.Join("testdata", "specs"))
	if err != nil {
		panic(err)
	}

	err = os.Setenv(SpecsDirEnv, fixture)
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}
