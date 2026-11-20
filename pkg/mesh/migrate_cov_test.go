// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func covMigrating(moves ...KVMove) *Definition {
	return &Definition{Name: "svc", Dir: "svc", Migrations: Migrations{KV: moves}}
}

func TestCovMigrate_kvFailuresDoNotStopTheOtherMoves(t *testing.T) {
	t.Parallel()

	moves := []KVMove{{Old: "a", New: "a2"}, {Old: "b", New: "b2"}}

	tests := []struct {
		name   string
		consul *covConsul
		want   string
		stored map[string]string
	}{
		{"read", &covConsul{getErr: errCov}, "unable to read a", map[string]string{}},
		{"write", &covConsul{kv: map[string]string{"a": "1", "b": "2"}, putErr: errCov}, "unable to write a2",
			map[string]string{"a": "1", "b": "2"}},
		{"delete", &covConsul{kv: map[string]string{"a": "1", "b": "2"}, deleteErr: errCov}, "unable to delete a",
			map[string]string{"a": "1", "b": "2", "a2": "1", "b2": "2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			machine.useConsul(tt.consul)

			err := Migrate(machine.env, covMigrating(moves...), "tok", noLog)
			require.ErrorIs(t, err, errCov)
			require.ErrorContains(t, err, tt.want)
			require.ErrorContains(t, err, "b", "the second move is attempted as well")
			assert.Equal(t, tt.stored, tt.consul.kvOrEmpty())
		})
	}
}

func (c *covConsul) kvOrEmpty() map[string]string {
	if c.kv == nil {
		return map[string]string{}
	}

	return c.kv
}

func TestCovMigrate_noAccessToConsul(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.env.Consul = func(string) (Consul, error) { return nil, errCov }

	require.ErrorIs(t, Migrate(machine.env, covMigrating(KVMove{Old: "a", New: "b"}), "tok", noLog), errCov)
}

func TestCovMigrate_propertiesFailuresAreReportedWithTheKVOnes(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	consul := &covConsul{getErr: errCov}
	machine.useConsul(consul)

	def := covMigrating(KVMove{Old: "a", New: "b"})
	def.Migrations.Properties = []PropertyMove{{Old: "x", New: "y"}}

	require.NoError(t, os.MkdirAll(def.PropertiesFile(machine.env), 0o755), "a directory instead of the file")

	err := Migrate(machine.env, def, "tok", noLog)
	require.ErrorIs(t, err, errCov)
	require.ErrorContains(t, err, "is a directory")
}

func TestCovMigrate_properties(t *testing.T) {
	t.Parallel()

	moves := []PropertyMove{{Old: "old.key", New: "new.key"}}

	tests := []struct {
		name    string
		content *string // nil: no file
		moves   []PropertyMove
		want    string
		wantErr string
		failOwn bool
		logged  int
	}{
		{name: "no file", moves: moves},
		{name: "nothing to move", content: ptr("other=1\n"), moves: moves, want: "other=1\n"},
		{name: "key moved", content: ptr("old.key=1\n"), moves: moves, want: "new.key=1\n", logged: 1},
		{name: "invalid URI", content: ptr("uri=http://[::1\n"), want: "uri=http://[::1\n", wantErr: "uri",
			moves: []PropertyMove{{Old: "uri", Host: "h", Port: "p"}}},
		{name: "port out of range", content: ptr("uri=http://h:99999999999999999999\n"),
			want: "uri=http://h:99999999999999999999\n", wantErr: "uri",
			moves: []PropertyMove{{Old: "uri", Host: "h", Port: "p"}}},
		{name: "file that cannot be replaced", content: ptr("old.key=1\n"), moves: moves,
			want: "old.key=1\n", wantErr: errCov.Error(), failOwn: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			def := &Definition{Name: "svc", Dir: "svc", Migrations: Migrations{Properties: tt.moves}}
			path := def.PropertiesFile(machine.env)

			if tt.content != nil {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(*tt.content), 0o640))
			}

			if tt.failOwn {
				machine.failChown(isTemporary)
			}

			logger := &covLogger{}

			err := Migrate(machine.env, def, "tok", logger.log)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Len(t, logger.lines, tt.logged)

			if tt.content != nil {
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				assert.Equal(t, tt.want, string(got))
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestCovMigrate_rewriteInPlaceNeedsTheFile(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)

	require.ErrorIs(t, rewriteInPlace(machine.env, filepath.Join(t.TempDir(), "missing"), nil), os.ErrNotExist)
}

func TestCovMigrate_propertyLines(t *testing.T) {
	t.Parallel()

	t.Run("key without value", func(t *testing.T) {
		t.Parallel()

		key, value, ok := propertyKey("onlykey")
		assert.True(t, ok)
		assert.Equal(t, "onlykey", key)
		assert.Empty(t, value)
	})

	t.Run("new key", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"a=1\n"}, setProperty([]string{"a=1"}, "a", "1"), "an existing key is replaced")
		assert.Equal(t, []string{"a=1\n", "b=2\n"}, setProperty([]string{"a=1"}, "b", "2"))
		assert.Equal(t, []string{"b=2\n"}, setProperty(nil, "b", "2"))
	})
}

func TestCovMigrate_escapeAndUnescape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw     string
		escaped string
	}{
		{"plain", "plain"},
		{`a\b=c:d#e!f g`, `a\\b\=c\:d\#e\!f\ g`},
		{"città", "città"},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.escaped, escapeProperty(tt.raw))
			assert.Equal(t, tt.raw, unescapeProperty(tt.escaped))
		})
	}

	escapes := []struct {
		name, escaped, want string
	}{
		{"tab", `a\tb`, "a\tb"},
		{"newline", `a\nb`, "a\nb"},
		{"carriage return", `a\rb`, "a\rb"},
		{"form feed", `a\fb`, "a\fb"},
		{"unicode", `\u0041\u00e8`, "Aè"},
		{"short unicode", `\u41`, "u41"},
		{"invalid unicode", `\uZZZZ!`, "uZZZZ!"},
		{"trailing backslash", `abc\`, `abc\`},
		{"unknown escape", `\q`, "q"},
	}

	for _, tt := range escapes {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, unescapeProperty(tt.escaped))
		})
	}
}
