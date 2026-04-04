// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package systemd

import (
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
		return errors.Errorf("unable to establish connection to any D-Bus\n%s", err)
	}
	defer dBusConn.Close()

	_, _, err = dBusConn.EnableUnitFiles([]string{unitName}, false, false)
	if err != nil {
		return err
	}

	return nil
}
