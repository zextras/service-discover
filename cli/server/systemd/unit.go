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
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
)

type UnitManager interface {
	StartUnit(name string, mode string, ch chan<- string) (int, error)
	StopUnit(name string, mode string, ch chan<- string) (int, error)
	ReloadUnit(name string, mode string, ch chan<- string) (int, error)
	RestartUnit(name string, mode string, ch chan<- string) (int, error)
	TryRestartUnit(name string, mode string, ch chan<- string) (int, error)
	ReloadOrRestartUnit(name string, mode string, ch chan<- string) (int, error)
	ReloadOrTryRestartUnit(name string, mode string, ch chan<- string) (int, error)
	StartTransientUnit(name string, mode string, properties []dbus.Property, ch chan<- string) (int, error)
	KillUnit(name string, signal int32)
	ResetFailedUnit(name string) error
	SystemState() (*dbus.Property, error)
	GetUnitProperty(unit string, propertyName string) (*dbus.Property, error)
	GetServiceProperty(service string, propertyName string) (*dbus.Property, error)
	GetUnitTypeProperties(unit string, unitType string) (map[string]interface{}, error)
	SetUnitProperties(name string, runtime bool, properties ...dbus.Property) error
	GetUnitTypeProperty(unit string, unitType string, propertyName string) (*dbus.Property, error)
	ListUnits() ([]dbus.UnitStatus, error)
	ListUnitsFiltered(states []string) ([]dbus.UnitStatus, error)
	ListUnitsByPatterns(states []string, patterns []string) ([]dbus.UnitStatus, error)
	ListUnitsByNames(units []string) ([]dbus.UnitStatus, error)
	ListUnitFiles() ([]dbus.UnitFile, error)
	ListUnitFilesByPatterns(states []string, patterns []string) ([]dbus.UnitFile, error)
	LinkUnitFiles(files []string, runtime bool, force bool) ([]dbus.LinkUnitFileChange, error)
	EnableUnitFiles(files []string, runtime bool, force bool) (bool, []dbus.EnableUnitFileChange, error)
	DisableUnitFiles(files []string, runtime bool) ([]dbus.DisableUnitFileChange, error)
	MaskUnitFiles(files []string, runtime bool, force bool) ([]dbus.MaskUnitFileChange, error)
	UnmaskUnitFiles(files []string, runtime bool) ([]dbus.UnmaskUnitFileChange, error)
	Reload() error
	NewSubscriptionSet() *dbus.SubscriptionSet
	Close()
	GetManagerProperty(prop string) (string, error)
	Subscribe() error
	Unsubscribe() error
	SubscribeUnits(interval time.Duration) (<-chan map[string]*dbus.UnitStatus, <-chan error)
	SubscribeUnitsCustom(interval time.Duration, buffer int, isChanged func(*dbus.UnitStatus, *dbus.UnitStatus) bool,
		filterUnit func(string) bool) (<-chan map[string]*dbus.UnitStatus, <-chan error)
	SetSubStateSubscriber(updateCh chan<- *dbus.SubStateUpdate, errCh chan<- error)
	SetPropertiesSubscriber(updateCh chan<- *dbus.PropertiesUpdate, errCh chan<- error)
}
