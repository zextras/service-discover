/*
 * Copyright (C) 2023 Zextras srl
 *
 *     This program is free software: you can redistribute it and/or modify
 *     it under the terms of the GNU Affero General Public License as published by
 *     the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU Affero General Public License for more details.
 *
 *     You should have received a copy of the GNU Affero General Public License
 *     along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 */

package permissions

import (
	"github.com/pkg/errors"
	"os"
	"os/user"
	"strconv"
)

type PermissionInterface interface {
	LookupUser(name string) (*user.User, error)
	LookupGroup(name string) (*user.Group, error)
	Chown(path string, userUid int, groupUid int) error
	Chmod(path string, mode os.FileMode) error
}

// SetStrictPermissions change permissions to 600 and change ownership, only of the specific
// path, to 'service-discover' user and group
func SetStrictPermissions(d PermissionInterface, path string) error {
	serviceDiscoverUser, err := d.LookupUser("service-discover")
	if err != nil {
		return errors.New("cannot find user service-discover: " + err.Error())
	}

	uid, err := strconv.Atoi(serviceDiscoverUser.Uid)
	if err != nil {
		return errors.New("cannot parse user id for service-discover: " + err.Error())
	}

	serviceDiscoverGroup, err := d.LookupGroup("service-discover")
	if err != nil {
		return errors.New("cannot find group service-discover: " + err.Error())
	}

	gid, err := strconv.Atoi(serviceDiscoverGroup.Gid)
	if err != nil {
		return errors.New("cannot parse group id for service-discover: " + err.Error())
	}

	err = d.Chown(path, uid, gid)
	if err != nil {
		return errors.New("cannot change ownership of '" + path + "': " + err.Error())
	}

	err = d.Chmod(path, 0600)
	if err != nil {
		return errors.New("cannot change permissions of '" + path + "': " + err.Error())
	}

	return nil
}
