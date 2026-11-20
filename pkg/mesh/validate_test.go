// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSpecs writes a directory of definitions: the index, and the policies and extra files of each service.
func writeSpecs(t *testing.T, services []map[string]any, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	raw, err := json.Marshal(map[string]any{"services": services})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), raw, 0o600))

	for _, service := range services {
		name, _ := service["name"].(string)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name, "policies.json"), []byte(`{"service":[{"`+name+`":{"policy":"write"}}]}`), 0o600))
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	return dir
}

func service(name string, extra map[string]any) map[string]any {
	def := map[string]any{
		"name": name, "dir": name, "user": "zextras", "pending_setup": "replace", "repo": "zextras/" + name,
		"hcl": []string{name + ".hcl"},
	}

	for key, value := range extra {
		def[key] = value
	}

	return def
}

func TestValidateDir_acceptsTheDefinitionsOfTheRules(t *testing.T) {
	t.Parallel()

	dir := writeSpecs(t, []map[string]any{
		service("carbonio-a", map[string]any{"sidecars": []string{"carbonio-a-sidecar.service"}}),
		service("carbonio-b", map[string]any{"hcl": []string{}, "consumer_only": true, "companions": []string{"carbonio-a"}}),
	}, nil)

	defs, err := ValidateDir(dir)

	require.NoError(t, err)
	assert.Len(t, defs, 2)
}

func TestValidateDir_reportsEveryProblem(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		def  map[string]any
		want string
	}{
		"no repository":     {service("carbonio-a", map[string]any{"repo": ""}), "the repository the files come from is required"},
		"bad sidecar":       {service("carbonio-a", map[string]any{"sidecars": []string{"proxy.service"}}), "*-sidecar.service: proxy.service"},
		"consumer with hcl": {service("carbonio-a", map[string]any{"consumer_only": true}), "registers nothing, it has no hcl"},
		"generated no hcl":  {service("carbonio-a", map[string]any{"generated": true, "hcl": []string{}}), "lists the hcl files its setup generates"},
		"unknown companion": {service("carbonio-a", map[string]any{"companions": []string{"nobody"}}), "unknown companion: nobody"},
		"empty command":     {service("carbonio-a", map[string]any{"post_setup": []string{"  "}}), "empty command"},
		"writes services": {
			service("carbonio-a", map[string]any{"extra_rules": map[string]any{"service_prefix": []any{map[string]any{"": map[string]string{"policy": "write"}}}}}),
			"access to the services of the extra policy must be read only",
		},
		"unregistered reads services": {
			service("carbonio-a", map[string]any{"hcl": []string{}, "extra_rules": map[string]any{"service_prefix": []any{map[string]any{"x-": map[string]string{"policy": "read"}}}}}),
			"registering nothing has no rule on the services",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ValidateDir(writeSpecs(t, []map[string]any{test.def}, nil))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "carbonio-a: ")
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestValidateDir_aBrokenConfigEntryAndAMissingDirectoryAreErrors(t *testing.T) {
	t.Parallel()

	def := service("carbonio-a", map[string]any{"files": []map[string]string{{"name": "service-protocol.json", "path": "x"}}})
	dir := writeSpecs(t, []map[string]any{def}, nil)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "carbonio-a", "service-protocol.json"), []byte(`{"Kind":"service-defaults","Protocol":"http"}`), 0o600))

	_, err := ValidateDir(dir)
	require.Error(t, err)

	_, err = ValidateDir(filepath.Join(dir, "nope"))
	require.ErrorIs(t, err, ErrSpecsMissing)
}
