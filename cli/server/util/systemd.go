package util

import (
	"bitbucket.org/zextras/service-discover/cli/server/systemd"
	"fmt"
	"github.com/pkg/errors"
)

func StartSystemdUnit(systemdHandler func() (systemd.UnitManager, error), unitName string) error {
	// Connect to systemd via dbus
	dBusConn, err := systemdHandler()
	if err != nil {
		return errors.New(fmt.Sprintf("unable to establish connection to any D-Bus\n%s", err))
	}
	defer dBusConn.Close()
	unitOutput := make(chan string, 1) // Channel with 1 inbox slot
	_, err = dBusConn.StartUnit(unitName, "replace", unitOutput)
	if err != nil {
		return errors.New("unable to launch systemd unit " + unitName)
	}
	// We block the execution until the channel send us back the systemd operation result
	if opOutput := <-unitOutput; opOutput != "done" {
		return errors.New("systemd unit startup finished with a code different than 'done'. Got " + opOutput)
	}
	return nil
}

func EnableSystemdUnit(systemdHandler func() (systemd.UnitManager, error), unitName string) error {
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
