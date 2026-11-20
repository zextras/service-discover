// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package permissions_test

import (
	"errors"
	"os"
	"os/user"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/permissions"
)

// fakeSystem records the ownership and mode changes, failing the step named in failAt.
type fakeSystem struct {
	user   *user.User
	group  *user.Group
	failAt string

	chownPath string
	chownUID  int
	chownGID  int
	chmodPath string
	chmodMode os.FileMode
}

func (f *fakeSystem) fail(step string) error {
	if f.failAt == step {
		return errors.New(step + " failed")
	}

	return nil
}

func (f *fakeSystem) LookupUser(string) (*user.User, error) {
	return f.user, f.fail("user")
}

func (f *fakeSystem) LookupGroup(string) (*user.Group, error) {
	return f.group, f.fail("group")
}

func (f *fakeSystem) Chown(path string, uid, gid int) error {
	f.chownPath, f.chownUID, f.chownGID = path, uid, gid

	return f.fail("chown")
}

func (f *fakeSystem) Chmod(path string, mode os.FileMode) error {
	f.chmodPath, f.chmodMode = path, mode

	return f.fail("chmod")
}

func newFakeSystem(failAt string) *fakeSystem {
	return &fakeSystem{user: &user.User{Uid: "10", Gid: "20"}, group: &user.Group{Gid: "30"}, failAt: failAt}
}

func TestSetStrictPermissions(t *testing.T) {
	t.Parallel()

	t.Run("gives the file to the service-discover user and group with mode 600", func(t *testing.T) {
		t.Parallel()

		system := newFakeSystem("")

		require.NoError(t, permissions.SetStrictPermissions(system, "/some/file"))

		assert.Equal(t, "/some/file", system.chownPath)
		assert.Equal(t, 10, system.chownUID)
		assert.Equal(t, 30, system.chownGID, "the group is the one of service-discover, not the primary group of the user")
		assert.Equal(t, "/some/file", system.chmodPath)
		assert.Equal(t, os.FileMode(0600), system.chmodMode)
	})

	tests := []struct {
		name    string
		failAt  string
		mutate  func(f *fakeSystem)
		wantErr string
	}{
		{name: "unknown user", failAt: "user", wantErr: "cannot find user service-discover: user failed"},
		{name: "non numeric uid", mutate: func(f *fakeSystem) { f.user.Uid = "x" }, wantErr: "cannot parse user id"},
		{name: "unknown group", failAt: "group", wantErr: "cannot find group service-discover: group failed"},
		{name: "non numeric gid", mutate: func(f *fakeSystem) { f.group.Gid = "x" }, wantErr: "cannot parse group id"},
		{name: "chown fails", failAt: "chown", wantErr: "cannot change ownership of '/some/file': chown failed"},
		{name: "chmod fails", failAt: "chmod", wantErr: "cannot change permissions of '/some/file': chmod failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			system := newFakeSystem(tt.failAt)
			if tt.mutate != nil {
				tt.mutate(system)
			}

			err := permissions.SetStrictPermissions(system, "/some/file")

			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSetOwnership(t *testing.T) {
	t.Parallel()

	t.Run("gives the file to the user and its primary group with the given mode", func(t *testing.T) {
		t.Parallel()

		system := newFakeSystem("")

		require.NoError(t, permissions.SetOwnership(system, "/some/file", "spire-server", 0640))

		assert.Equal(t, 10, system.chownUID)
		assert.Equal(t, 20, system.chownGID, "the group is the primary group of the user")
		assert.Equal(t, os.FileMode(0640), system.chmodMode)
	})

	tests := []struct {
		name    string
		failAt  string
		mutate  func(f *fakeSystem)
		wantErr string
	}{
		{name: "unknown user", failAt: "user", wantErr: "cannot find user spire-server: user failed"},
		{name: "non numeric uid", mutate: func(f *fakeSystem) { f.user.Uid = "x" }, wantErr: "cannot parse user id for spire-server"},
		{name: "non numeric gid", mutate: func(f *fakeSystem) { f.user.Gid = "x" }, wantErr: "cannot parse group id for spire-server"},
		{name: "chown fails", failAt: "chown", wantErr: "cannot change ownership of '/some/file': chown failed"},
		{name: "chmod fails", failAt: "chmod", wantErr: "cannot change permissions of '/some/file': chmod failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			system := newFakeSystem(tt.failAt)
			if tt.mutate != nil {
				tt.mutate(system)
			}

			err := permissions.SetOwnership(system, "/some/file", "spire-server", 0640)

			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
