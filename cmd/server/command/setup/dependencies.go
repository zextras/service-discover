// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"context"
	"io"
	"net"
	"os"
	"os/user"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/systemd"
	"github.com/zextras/service-discover/pkg/term"
)

type interactiveDependencies interface {
	Term() term.Terminal
	NetInterfaces() ([]net.Interface, error)
	AddrResolver(n net.Interface) ([]net.Addr, error)
	LookupIP(s string) ([]net.IP, error)
}

type businessDependencies interface {
	NetInterfaces() ([]net.Interface, error)
	AddrResolver(n net.Interface) ([]net.Addr, error)
	LookupIP(s string) ([]net.IP, error)
	LdapHandler(carbonio.LocalConfig) carbonio.LdapHandler
	LocalConfigLoader(path string) (carbonio.LocalConfig, error)
	SystemdUnitHandler() (systemd.UnitManager, error)
	CreateCommand(name string, args ...string) exec.Cmd
	GetuidSyscall() int
	LookupUser(name string) (*user.User, error)
	LookupGroup(name string) (*user.Group, error)
	Chown(path string, userUid int, groupUid int) error
	Chmod(path string, mode os.FileMode) error
}

type realDependencies struct {
	ui *term.Terminal
}

func (r realDependencies) Term() term.Terminal {
	return *r.ui
}

func (r realDependencies) Writer() io.Writer {
	return os.Stdout
}

func (r realDependencies) Reader() io.Reader {
	return os.Stdin
}

func (r realDependencies) NetInterfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

func (r realDependencies) AddrResolver(n net.Interface) ([]net.Addr, error) {
	return n.Addrs()
}

func (r realDependencies) LookupIP(s string) ([]net.IP, error) {
	return net.LookupIP(s)
}

func (r realDependencies) LdapHandler(config carbonio.LocalConfig) carbonio.LdapHandler {
	return carbonio.CreateNewHandler(config)
}

func (r realDependencies) LocalConfigLoader(path string) (carbonio.LocalConfig, error) {
	return carbonio.LoadLocalConfig(path)
}

func (r realDependencies) SystemdUnitHandler() (systemd.UnitManager, error) {
	return dbus.NewWithContext(context.Background())
}

func (r realDependencies) CreateCommand(name string, args ...string) exec.Cmd {
	return exec.Command(name, args...)
}

func (r realDependencies) GetuidSyscall() int {
	return os.Getuid()
}

func (r realDependencies) LookupUser(name string) (*user.User, error) {
	return user.Lookup(name)
}

func (r realDependencies) LookupGroup(name string) (*user.Group, error) {
	return user.LookupGroup(name)
}

func (r realDependencies) Chown(path string, userUid int, groupUid int) error {
	return os.Chown(path, userUid, groupUid)
}

func (r realDependencies) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}
