// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFS_rejectsAMalformedIndexAndAnUnknownSetupStep(t *testing.T) {
	t.Parallel()

	files := map[string]string{"a/policies.json": "{}"}
	service := `{"name":"a","dir":"a","user":"u","pending_setup":"replace","setup":"nonexistent-step","files":[{"name":"policies.json"}]}`

	_, err := LoadFS(definitionFS(`{"services":`, files))
	require.ErrorContains(t, err, "unable to decode the definitions index")

	_, err = LoadFS(definitionFS(`{"services":[`+service+`]}`, files))
	require.ErrorIs(t, err, errUnknownStep)
	require.ErrorIs(t, err, errInvalidName)
}
