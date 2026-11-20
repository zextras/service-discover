// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package meshtest_test

import (
	"os"
	"testing"

	"github.com/zextras/service-discover/pkg/mesh"
	"github.com/zextras/service-discover/pkg/mesh/meshtest"
)

func TestUseFixture_pointsTheLoaderAtTheFixture(t *testing.T) {
	t.Setenv(mesh.SpecsDirEnv, "")
	os.Unsetenv(mesh.SpecsDirEnv)

	meshtest.UseFixture()

	defs, err := mesh.Load()
	if err != nil {
		t.Fatalf("the fixture does not load: %v", err)
	}

	if len(defs) == 0 {
		t.Fatal("the fixture has no definition")
	}
}
