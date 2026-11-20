// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"os"
	"testing"

	"github.com/zextras/service-discover/pkg/mesh/meshtest"
)

func TestMain(m *testing.M) {
	meshtest.UseFixture()

	os.Exit(m.Run())
}
