// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

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
	GetUnitTypeProperties(unit string, unitType string) (map[string]any, error)
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
