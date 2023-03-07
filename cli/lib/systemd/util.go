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

package systemd

import (
	"fmt"
	"github.com/pkg/errors"
)

func StartSystemdUnit(systemdHandler func() (UnitManager, error), unitName string) error {
	// Connect to systemd via dbus
	dBusConn, err := systemdHandler()
	if err != nil {
		return errors.WithMessage(err, "unable to establish connection to any D-Bus")
	}
	defer dBusConn.Close()
	unitOutput := make(chan string, 1) // Channel with 1 inbox slot
	_, err = dBusConn.StartUnit(unitName, "replace", unitOutput)
	if err != nil {
		return errors.New("unable to launch systemd unit " + unitName)
	}
	// We block the execution until the channel send us back the systemd operation result
	if opOutput := <-unitOutput; opOutput != "done" {
		return errors.New("systemd unit startup finished with a code different than 'done'. Systemd returned: " + opOutput)
	}
	return nil
}

func EnableSystemdUnit(systemdHandler func() (UnitManager, error), unitName string) error {
	dBusConn, err := systemdHandler()
	if err != nil {
		return errors.New(fmt.Sprintf("unable to establish connection to any D-Bus\n%s", err))
	}
	defer dBusConn.Close()
	if _, _, err = dBusConn.EnableUnitFiles([]string{unitName}, false, false); err != nil {
		return err
	}
	return nil
}
