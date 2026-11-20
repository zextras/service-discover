// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_embeddedDefinitionsAreConsistent(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)
	require.NotEmpty(t, defs)

	names := map[string]bool{}

	for i := range defs {
		def := &defs[i]

		assert.False(t, names[def.Name], "duplicate %s", def.Name)

		names[def.Name] = true

		assert.NotEmpty(t, def.User, def.Name)
		assert.NotEmpty(t, def.Repo, def.Name)
		assert.NotEmpty(t, def.Files, def.Name)

		policy, err := def.Policy()
		require.NoError(t, err, def.Name)
		assert.True(t, json.Valid([]byte(policy)), "%s: policies.json is not JSON", def.Name)

		entries, err := def.ConfigEntries()
		require.NoError(t, err, def.Name)

		for _, entry := range entries {
			assert.NotEmpty(t, entry.GetName(), def.Name)
		}

		for _, unit := range def.Sidecars {
			assert.True(t, strings.HasSuffix(unit, "-sidecar.service"), "%s: %s", def.Name, unit)
		}

		if def.ConsumerOnly {
			assert.Empty(t, def.HCL, "%s registers nothing", def.Name)
		}
	}

	assert.True(t, slices.IsSortedFunc(defs, func(a, b Definition) int { return strings.Compare(a.Name, b.Name) }), "sorted by name")
}

func TestDefinition_identityNames(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	def, found := Find(defs, "carbonio-files")
	require.True(t, found)

	assert.Equal(t, "carbonio-files-policy", def.PolicyName())
	assert.Equal(t, "carbonio-files-role", def.RoleName())
	assert.Equal(t, "spiffe://carbonio/service/carbonio-files", def.SpiffeID("carbonio"))
	assert.Equal(t, `value.spiffe_id == "spiffe://carbonio.lab/service/carbonio-files"`, def.Selector("carbonio.lab"))
	assert.Equal(t, "unix:user:carbonio-files", def.UnixSelector())
	assert.Equal(t, "/etc/carbonio/files/service-discover/token", def.TokenFile())

	_, found = Find(defs, "no-such-service")
	assert.False(t, found)
}

func TestDefinition_configEntriesOrder(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	def, _ := Find(defs, "carbonio-files")

	entries, err := def.ConfigEntries()
	require.NoError(t, err)

	kinds := make([]string, 0, len(entries))
	for _, entry := range entries {
		kinds = append(kinds, entry.GetKind())
	}

	assert.Equal(t, []string{api.ServiceDefaults, api.ServiceRouter, api.ServiceIntentions}, kinds)
}

func definitionFS(index string, files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{indexFile: {Data: []byte(index)}}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}

	return fsys
}

func TestLoadFS_rejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()

	files := map[string]string{"a/policies.json": "{}", "a/service-protocol.json": `{"Kind":"service-defaults","Name":"a","Protocol":"http"}`}
	service := `{"name":"a","dir":"a","user":"u","pending_setup":"replace","files":[{"name":"policies.json"},{"name":"service-protocol.json"}]}`

	defs, err := LoadFS(definitionFS(`{"services":[`+service+`]}`, files))
	require.NoError(t, err)
	require.Len(t, defs, 1)

	_, err = LoadFS(definitionFS(`{"services":[`+service+`,`+service+`]}`, files))
	require.ErrorIs(t, err, errDuplicate)

	_, err = LoadFS(definitionFS(`{"services":[`+strings.Replace(service, `"replace"`, `"maybe"`, 1)+`]}`, files))
	require.ErrorIs(t, err, errInvalidName)

	_, err = LoadFS(definitionFS(`{"services":[`+strings.Replace(service, `"user":"u",`, "", 1)+`]}`, files))
	require.ErrorIs(t, err, errInvalidName)

	_, err = LoadFS(definitionFS(`{"services":[`+service+`]}`, map[string]string{}))
	require.ErrorIs(t, err, errNoPolicies)

	broken := map[string]string{"a/policies.json": "{}", "a/service-protocol.json": `{"Kind":"nonsense"}`}
	_, err = LoadFS(definitionFS(`{"services":[`+service+`]}`, broken))
	require.Error(t, err)

	_, err = LoadFS(fstest.MapFS{})
	require.Error(t, err)
}
