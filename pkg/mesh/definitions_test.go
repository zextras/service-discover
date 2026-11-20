// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_fixtureDefinitionsAreConsistent(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)
	require.NotEmpty(t, defs)

	_, err = ValidateDir(filepath.Join("testdata", "specs"))
	require.NoError(t, err)

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

func TestReadsAllServices(t *testing.T) {
	t.Parallel()

	read := map[string]Rule{"": {Policy: "read"}}

	assert.True(t, readsAllServices(Rules{"service_prefix": {read}}))
	assert.True(t, readsAllServices(Rules{"service_prefix": {{"": {Policy: "write"}}}}))
	assert.False(t, readsAllServices(Rules{"service_prefix": {{"carbonio-": {Policy: "read"}}}}))
	assert.False(t, readsAllServices(Rules{"service_prefix": {{"": {Policy: "deny"}}}}))
	assert.False(t, readsAllServices(Rules{"service": {read}}))
	assert.False(t, readsAllServices(Rules{}))
}

// RabbitMQ registers its own node in Consul and takes the lock of the cluster formation under rabbitmq/, with the
// token of the service: without it the node registers once, at its first start, and never again.
func TestDefinitions_messageBrokerTakesTheClusterFormationLock(t *testing.T) {
	t.Parallel()

	broker, found := Find(loadDefs(t), "carbonio-message-broker")
	require.True(t, found)

	rules, err := broker.ExtraPolicy()
	require.NoError(t, err)
	assert.Contains(t, rules, `"rabbitmq/"`)
}
