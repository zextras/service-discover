// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// KVMove renames a key of the Consul KV store: the value of Old, when the key exists, is stored under New and Old is
// deleted. Several moves of a definition run in the order they are listed.
type KVMove struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// PropertyMove renames a key of the config.properties file of the service (/etc/carbonio/<dir>/config.properties).
// The value of Old, when the key exists, is stored under New and the key is removed. When URI is set the value is a
// URI instead, and its host and port are stored under the keys named by Host and Port.
type PropertyMove struct {
	Old  string `json:"old"`
	New  string `json:"new,omitempty"`
	Host string `json:"host,omitempty"`
	Port string `json:"port,omitempty"`
}

// Migrations are the changes to the configuration of a service that its upgrades need. They are the ones the Java
// services run with their --setup command, that cannot run without the bootstrap token: a migration only touches
// keys of the service, so it is done here with the token of the service, on the nodes using SPIRE.
type Migrations struct {
	KV         []KVMove       `json:"kv"`
	Properties []PropertyMove `json:"properties"`
}

// Empty reports whether there is nothing to migrate.
func (m *Migrations) Empty() bool {
	return len(m.KV) == 0 && len(m.Properties) == 0
}

// Keys returns the keys of the KV store that a migration reads, writes or deletes.
func (m *Migrations) Keys() []string {
	keys := make([]string, 0, 2*len(m.KV))

	for _, move := range m.KV {
		keys = appendUnique(keys, move.Old, move.New)
	}

	return keys
}

// KV is the part of the Consul KV store the migrations use.
type KV interface {
	// Get returns the value of the key, false when it does not exist.
	Get(key string) (string, bool, error)
	Put(key, value string) error
	Delete(key string) error
}

// PropertiesFile returns the path of the config.properties file of the service.
func (d *Definition) PropertiesFile(env *Env) string {
	return filepath.Join(env.CarbonioDir, d.Dir, "config.properties")
}

// Migrate runs the migrations of the definition. A key that does not exist is skipped, so on a new installation, or
// when the migration has already been done, nothing happens. A failure does not stop the other moves: they are
// reported together, and a failed move is retried by the next run.
func Migrate(env *Env, def *Definition, token string, log func(string, ...any)) error {
	if def.Migrations.Empty() {
		return nil
	}

	var failures []error

	if len(def.Migrations.KV) > 0 {
		consul, err := env.Consul(token)
		if err != nil {
			return err
		}

		failures = append(failures, migrateKV(consul, def, log)...)
	}

	if len(def.Migrations.Properties) > 0 {
		err := migrateProperties(env, def, log)
		if err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

func migrateKV(store KV, def *Definition, log func(string, ...any)) []error {
	var failures []error

	for _, move := range def.Migrations.KV {
		value, found, err := store.Get(move.Old)
		if err != nil {
			failures = append(failures, fmt.Errorf("unable to read %s: %w", move.Old, err))

			continue
		}

		if !found {
			continue
		}

		err = store.Put(move.New, value)
		if err != nil {
			failures = append(failures, fmt.Errorf("unable to write %s: %w", move.New, err))

			continue
		}

		err = store.Delete(move.Old)
		if err != nil {
			failures = append(failures, fmt.Errorf("unable to delete %s: %w", move.Old, err))

			continue
		}

		log("%s: KV %s moved to %s", def.Name, move.Old, move.New)
	}

	return failures
}

func migrateProperties(env *Env, def *Definition, log func(string, ...any)) error {
	path := def.PropertiesFile(env)

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	edited, moved, err := MoveProperties(raw, def.Migrations.Properties)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	if len(moved) == 0 {
		return nil
	}

	err = rewriteInPlace(env, path, edited)
	if err != nil {
		return err
	}

	for _, key := range moved {
		log("%s: property %s migrated in %s", def.Name, key, path)
	}

	return nil
}

// rewriteInPlace replaces the content of a file keeping its owner and mode.
func rewriteInPlace(env *Env, path string, content []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	account := Account{}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok {
		account = Account{UID: int(stat.Uid), GID: int(stat.Gid)}
	}

	return writeFile(env, path, content, info.Mode().Perm(), account)
}

// MoveProperties applies the moves to the content of a Java properties file and returns the new content together
// with the old keys that have been migrated. Lines that are not touched are kept as they are, comments included.
func MoveProperties(content []byte, moves []PropertyMove) ([]byte, []string, error) {
	lines := strings.SplitAfter(string(content), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var moved []string

	for _, move := range moves {
		value, found := propertyValue(lines, move.Old)
		if !found {
			continue
		}

		set, err := newProperties(move, value)
		if err != nil {
			return nil, nil, err
		}

		lines = removeProperty(lines, move.Old)

		for _, kv := range set {
			lines = setProperty(lines, kv[0], kv[1])
		}

		moved = append(moved, move.Old)
	}

	return []byte(strings.Join(lines, "")), moved, nil
}

// newProperties returns the key and the (escaped) value pairs the move stores.
func newProperties(move PropertyMove, escaped string) ([][2]string, error) {
	if move.Host == "" {
		return [][2]string{{move.New, escaped}}, nil
	}

	uri, err := url.Parse(unescapeProperty(escaped))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", move.Old, err)
	}

	// A URI without a port gives -1 in Java, that is what the services read.
	port := -1

	if uri.Port() != "" {
		port, err = strconv.Atoi(uri.Port())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", move.Old, err)
		}
	}

	return [][2]string{{move.Host, escapeProperty(uri.Hostname())}, {move.Port, strconv.Itoa(port)}}, nil
}

// propertyKey returns the key of a properties line, false for comments, blank lines and continuations.
func propertyKey(line string) (string, string, bool) {
	trimmed := strings.TrimLeft(line, " \t\f")
	if trimmed == "" || trimmed[0] == '#' || trimmed[0] == '!' || trimmed[0] == '\r' || trimmed[0] == '\n' {
		return "", "", false
	}

	end := strings.IndexAny(trimmed, "=: \t\f\r\n")
	if end < 0 {
		return unescapeProperty(trimmed), "", true
	}

	key := unescapeProperty(trimmed[:end])
	rest := strings.TrimLeft(trimmed[end:], " \t\f")

	if rest != "" && (rest[0] == '=' || rest[0] == ':') {
		rest = strings.TrimLeft(rest[1:], " \t\f")
	}

	return key, strings.TrimRight(rest, "\r\n"), true
}

func propertyValue(lines []string, key string) (string, bool) {
	value, found := "", false

	for _, line := range lines {
		k, v, ok := propertyKey(line)
		if ok && k == key {
			value, found = v, true
		}
	}

	return value, found
}

func removeProperty(lines []string, key string) []string {
	kept := lines[:0:0]

	for _, line := range lines {
		k, _, ok := propertyKey(line)
		if !ok || k != key {
			kept = append(kept, line)
		}
	}

	return kept
}

func setProperty(lines []string, key, value string) []string {
	entry := escapeProperty(key) + "=" + value + "\n"

	for i, line := range lines {
		k, _, ok := propertyKey(line)
		if ok && k == key {
			lines[i] = entry

			return lines
		}
	}

	if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
		lines[n-1] += "\n"
	}

	return append(lines, entry)
}

// escapeProperty escapes a string the way java.util.Properties stores it.
func escapeProperty(s string) string {
	var out bytes.Buffer

	for _, char := range s {
		switch char {
		case '\\', '=', ':', '#', '!', ' ':
			out.WriteByte('\\')
		}

		out.WriteRune(char)
	}

	return out.String()
}

// unescapeProperty reverses escapeProperty, and understands the other escapes of the format.
func unescapeProperty(escaped string) string {
	if !strings.Contains(escaped, `\`) {
		return escaped
	}

	var out strings.Builder

	for pos := 0; pos < len(escaped); pos++ {
		if escaped[pos] != '\\' || pos+1 == len(escaped) {
			out.WriteByte(escaped[pos])

			continue
		}

		pos++

		switch escaped[pos] {
		case 't':
			out.WriteByte('\t')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 'f':
			out.WriteByte('\f')
		case 'u':
			if pos+4 < len(escaped) {
				code, err := strconv.ParseUint(escaped[pos+1:pos+5], 16, 16)
				if err == nil {
					out.WriteRune(rune(code))

					pos += 4

					continue
				}
			}

			out.WriteByte('u')
		default:
			out.WriteByte(escaped[pos])
		}
	}

	return out.String()
}
