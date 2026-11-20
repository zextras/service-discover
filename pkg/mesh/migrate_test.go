// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoveProperties(t *testing.T) {
	t.Parallel()

	moves := []PropertyMove{
		{Old: "carbonio.postgres.host", New: "carbonio.postgresql.host"},
		{Old: "carbonio.postgres.port", New: "carbonio.postgresql.port"},
		{Old: "carbonio.mailbox.url", Host: "carbonio.mailbox.host", Port: "carbonio.mailbox.port"},
		{Old: "absent", New: "never"},
	}

	input := "# comment\ncarbonio.postgres.host = db.example.com\nother=1\ncarbonio.postgres.port:5432\n" +
		"carbonio.mailbox.url=https://mbox.example.com:7071/service\n"

	edited, moved, err := MoveProperties([]byte(input), moves)
	require.NoError(t, err)

	assert.Equal(t, []string{"carbonio.postgres.host", "carbonio.postgres.port", "carbonio.mailbox.url"}, moved)
	assert.Equal(t, "# comment\nother=1\ncarbonio.postgresql.host=db.example.com\ncarbonio.postgresql.port=5432\n"+
		"carbonio.mailbox.host=mbox.example.com\ncarbonio.mailbox.port=7071\n", string(edited))

	again, moved, err := MoveProperties(edited, moves)
	require.NoError(t, err)
	assert.Empty(t, moved, "already migrated")
	assert.Equal(t, string(edited), string(again))
}

func TestMoveProperties_uriWithoutAPortAndNoFinalNewline(t *testing.T) {
	t.Parallel()

	moves := []PropertyMove{{Old: "carbonio.mailbox.url", Host: "carbonio.mailbox.host", Port: "carbonio.mailbox.port"}}

	edited, moved, err := MoveProperties([]byte("a=b\ncarbonio.mailbox.url=http://mbox"), moves)
	require.NoError(t, err)
	assert.Equal(t, []string{"carbonio.mailbox.url"}, moved)
	assert.Equal(t, "a=b\ncarbonio.mailbox.host=mbox\ncarbonio.mailbox.port=-1\n", string(edited))

	edited, moved, err = MoveProperties(nil, moves)
	require.NoError(t, err)
	assert.Empty(t, moved)
	assert.Empty(t, edited)
}

func TestMoveProperties_keepsAnExistingNewKeyOnce(t *testing.T) {
	t.Parallel()

	edited, _, err := MoveProperties([]byte("old=1\nnew=0\n"), []PropertyMove{{Old: "old", New: "new"}})
	require.NoError(t, err)
	assert.Equal(t, "new=1\n", string(edited))
}

func TestMoveProperties_invalidPort(t *testing.T) {
	t.Parallel()

	_, _, err := MoveProperties([]byte("u=http://h:notaport\n"), []PropertyMove{{Old: "u", Host: "h", Port: "p"}})
	require.Error(t, err)
}

func TestSync_setupMovesTheKVKeysWithTheTokenOfTheService(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	machine.kv["carbonio-files/db-name"] = "files"
	machine.kv["carbonio-files/hikari-max-pool-size"] = "20"
	machine.kv["carbonio-files/database/credentials/db-username"] = "kept"

	out := &bytes.Buffer{}

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Setup: true, Out: out}))

	assert.Equal(t, "files", machine.kv["carbonio-files/database/credentials/db-name"])
	assert.Equal(t, "20", machine.kv["carbonio-files/database/db-pool-max-size"])
	assert.Equal(t, "kept", machine.kv["carbonio-files/database/credentials/db-username"])
	assert.NotContains(t, machine.kv, "carbonio-files/db-name")
	assert.NotContains(t, machine.kv, "carbonio-files/hikari-max-pool-size")
	assert.Contains(t, out.String(), "KV carbonio-files/db-name moved to carbonio-files/database/credentials/db-name")
	assert.Equal(t, []string{"secret-1"}, machine.kvTokens, "the token of the service, not the bootstrap one")
}

func TestSync_withoutSetupNothingIsMigrated(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	machine.kv["carbonio-files/db-name"] = "files"

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	assert.Equal(t, map[string]string{"carbonio-files/db-name": "files"}, machine.kv)
	assert.Empty(t, machine.kvTokens)
}

func TestSync_setupMigratesThePropertiesKeepingTheOwner(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.users["carbonio-user-management"] = Account{UID: 1010, GID: 1010}

	require.NoError(t, os.WriteFile(filepath.Join(machine.env.ServicesDir, "carbonio-user-management.hcl"),
		[]byte("services {\n  name = \"carbonio-user-management\"\n}\n"), 0o644))

	path := filepath.Join(machine.env.CarbonioDir, "user-management", "config.properties")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("carbonio.user-management.host=0.0.0.0\ncarbonio.user-management.port=10001\n"), 0o640))

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Setup: true}))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "carbonio.service.host=0.0.0.0\ncarbonio.service.port=10001\n", string(content))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	assert.NotZero(t, machine.chowns[filepath.Dir(path)], "the owner is given back")
}

func TestDefinition_extraPolicyListsTheMovedKeys(t *testing.T) {
	t.Parallel()

	defs := loadDefs(t)

	files, _ := Find(defs, "carbonio-files")

	rules, err := files.ExtraPolicy()
	require.NoError(t, err)
	assert.Contains(t, rules, `"carbonio-files/database/credentials/db-name"`)
	assert.Contains(t, rules, `"carbonio-files/db-name"`)

	proxy, _ := Find(defs, "carbonio-proxy")

	rules, err = proxy.ExtraPolicy()
	require.NoError(t, err)
	assert.JSONEq(t, `{"service_prefix": [{"": {"policy": "read"}}]}`, rules, "only the read access to the upstreams")
}
