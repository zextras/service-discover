// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetup_spireServerAddress_failures(t *testing.T) {
	t.Run("hosts file cannot be written with more than one server", func(t *testing.T) {
		f := newJoinFixture(t, nil)
		dialSpireServer = func(string) error { return nil }

		require.NoError(t, os.Remove(f.dirs.hosts))
		require.NoError(t, os.MkdirAll(f.dirs.hosts, 0755))

		_, err := f.setup.spireServerAddress(f.deps, "10.0.0.1", 8081, []string{"10.0.0.2:8300"})
		require.ErrorContains(t, err, "unable to map "+spireServersAlias)
	})

	t.Run("consul server without port", func(t *testing.T) {
		f := newJoinFixture(t, nil)
		dialSpireServer = func(address string) error {
			if address == "10.0.0.2:8081" {
				return nil
			}

			return errors.New("connection refused")
		}

		address, err := f.setup.spireServerAddress(f.deps, "10.0.0.1", 8081, []string{"10.0.0.2"})
		require.NoError(t, err)
		assert.Equal(t, "10.0.0.2", address)
	})
}

func TestResolveHost(t *testing.T) {
	f := newJoinFixture(t, nil)
	f.deps.On("LookupIP", "unknown.example.com").Return(nil, errors.New("no such host"))

	tests := []struct {
		name string
		host string
		want []string
	}{
		{name: "address", host: "10.0.0.7", want: []string{"10.0.0.7"}},
		{name: "name", host: "mail.example.com", want: []string{"10.0.0.1"}},
		{name: "unknown name", host: "unknown.example.com", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveHost(f.deps, tt.host))
		})
	}

	f.deps.AssertNotCalled(t, "LookupIP", "10.0.0.7")
}

func TestHostsBlock_failures(t *testing.T) {
	directory := t.TempDir()

	t.Run("write: hosts file unreadable", func(t *testing.T) {
		require.Error(t, writeHostsBlock(directory, spireServersAlias, []string{"10.0.0.1"}))
	})

	t.Run("remove: hosts file unreadable", func(t *testing.T) {
		require.Error(t, removeHostsBlock(directory))
	})

	t.Run("remove: no hosts file", func(t *testing.T) {
		require.NoError(t, removeHostsBlock(filepath.Join(directory, "missing")))
	})

	t.Run("write: hosts file created when missing", func(t *testing.T) {
		path := filepath.Join(directory, "created")

		require.NoError(t, writeHostsBlock(path, spireServersAlias, []string{"10.0.0.1"}))
		assert.Equal(t, hostsBegin+"\n10.0.0.1 "+spireServersAlias+"\n"+hostsEnd+"\n", string(mustReadFile(t, path)))
	})
}

func TestReplaceFile(t *testing.T) {
	t.Run("keeps the mode of the file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hosts")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0640))

		require.NoError(t, replaceFile(path, []byte("new")))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0640), info.Mode().Perm())
		assert.Equal(t, "new", string(mustReadFile(t, path)))
	})

	t.Run("follows a symbolic link", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		link := filepath.Join(dir, "link")
		require.NoError(t, os.WriteFile(target, []byte("old"), 0644))
		require.NoError(t, os.Symlink(target, link))

		require.NoError(t, replaceFile(link, []byte("new")))

		assert.Equal(t, "new", string(mustReadFile(t, target)))
		resolved, err := os.Readlink(link)
		require.NoError(t, err)
		assert.Equal(t, target, resolved)
	})

	t.Run("creates the file with the default mode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hosts")

		require.NoError(t, replaceFile(path, []byte("new")))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(hostsFileMode), info.Mode().Perm())
	})

	t.Run("directory of the file missing", func(t *testing.T) {
		require.Error(t, replaceFile(filepath.Join(t.TempDir(), "missing", "hosts"), []byte("new")))
	})

	t.Run("target is a directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "hosts")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "child"), 0755))

		require.Error(t, replaceFile(dir, []byte("new")))

		entries, err := os.ReadDir(filepath.Dir(dir))
		require.NoError(t, err)
		assert.Len(t, entries, 1, "the temporary file is removed")
	})
}

func TestDialSpireServer_real(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := listener.Addr().String()

	require.NoError(t, dialSpireServer(address))

	require.NoError(t, listener.Close())
	require.Error(t, dialSpireServer(address), "nothing listens any more")
}
