// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const blockService = `// SPDX-License-Identifier: AGPL-3.0-only

services {
  check {
    http = "http://127.78.0.2:10000/q/health/live"
  }
  # keep this comment
  name = "carbonio-files"
  port = 10000
}
`

const objectService = `service = {
  "name" = "carbonio-prometheus-rabbit-exporter"
  "tags" = ["prometheus-exporter"]
  "port" = 15692
}
`

const listService = `services = [
  {
    name = "a"
    port = 1
  },
  {
    name = "b"
    port = 2
  }
]
`

func parsesAsHCL(t *testing.T, src []byte) {
	t.Helper()

	_, diags := hclsyntax.ParseConfig(src, "test.hcl", hcl.InitialPos)
	require.False(t, diags.HasErrors(), "%s\n%s", diags, src)
}

func TestInjectToken_blockForm(t *testing.T) {
	t.Parallel()

	got, err := InjectToken([]byte(blockService), "carbonio-files.hcl", "s3cr3t")
	require.NoError(t, err)

	parsesAsHCL(t, got)
	assert.Contains(t, string(got), `token = "s3cr3t"`)
	assert.Contains(t, string(got), "# keep this comment")
	assert.Contains(t, string(got), "// SPDX-License-Identifier")
	assert.Contains(t, string(got), `http = "http://127.78.0.2:10000/q/health/live"`)
}

func TestInjectToken_blockFormReplacesAnExistingToken(t *testing.T) {
	t.Parallel()

	first, err := InjectToken([]byte(blockService), "x.hcl", "old")
	require.NoError(t, err)

	second, err := InjectToken(first, "x.hcl", "new")
	require.NoError(t, err)

	assert.Contains(t, string(second), `"new"`)
	assert.NotContains(t, string(second), `"old"`)
}

func TestInjectToken_objectForm(t *testing.T) {
	t.Parallel()

	got, err := InjectToken([]byte(objectService), "exporter.hcl", "s3cr3t")
	require.NoError(t, err)

	parsesAsHCL(t, got)
	assert.Contains(t, string(got), `"token" = "s3cr3t"`)
	assert.Contains(t, string(got), `"port" = 15692`)
}

func TestInjectToken_objectFormWithTokenIsRejected(t *testing.T) {
	t.Parallel()

	got, err := InjectToken([]byte(objectService), "exporter.hcl", "s3cr3t")
	require.NoError(t, err)

	_, err = InjectToken(got, "exporter.hcl", "again")
	require.ErrorIs(t, err, errTokenDefined)
}

func TestInjectToken_listOfObjects(t *testing.T) {
	t.Parallel()

	got, err := InjectToken([]byte(listService), "list.hcl", "s3cr3t")
	require.NoError(t, err)

	parsesAsHCL(t, got)
	assert.Equal(t, 2, countOccurrences(string(got), `"token" = "s3cr3t"`), "every service gets the token")
}

func countOccurrences(s, sub string) int {
	count := 0

	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			count++
		}
	}

	return count
}

func TestInjectToken_errors(t *testing.T) {
	t.Parallel()

	_, err := InjectToken([]byte(`foo = "bar"`), "x.hcl", "t")
	require.ErrorIs(t, err, errNoService)

	_, err = InjectToken([]byte(`service = "bar"`), "x.hcl", "t")
	require.ErrorIs(t, err, errUnsupportedHCL)

	_, err = InjectToken([]byte(`services = ["a"]`), "x.hcl", "t")
	require.ErrorIs(t, err, errUnsupportedHCL)

	_, err = InjectToken([]byte(`services {`), "x.hcl", "t")
	require.Error(t, err)
}

func TestInjectToken_leavesOtherBlocksAlone(t *testing.T) {
	t.Parallel()

	src := "check {\n  http = \"http://127.0.0.1/health\"\n}\n\n" + blockService

	got, err := InjectToken([]byte(src), "carbonio-files.hcl", "s3cr3t")
	require.NoError(t, err)

	parsesAsHCL(t, got)
	assert.Equal(t, 1, countOccurrences(string(got), `token = "s3cr3t"`), "only the service gets the token")
	assert.Contains(t, string(got), "check {\n  http = \"http://127.0.0.1/health\"\n}")
}
