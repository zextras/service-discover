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

// InteractiveDependencies defines the interface for interactive terminal operations
// used during setup wizards.
type InteractiveDependencies interface {
	Term() term.Terminal
	NetInterfaces() ([]net.Interface, error)
	AddrResolver(n net.Interface) ([]net.Addr, error)
	LookupIP(s string) ([]net.IP, error)
}

// BusinessDependencies defines the interface for business logic operations
// used during the setup process.
type BusinessDependencies interface {
	NetInterfaces() ([]net.Interface, error)
	AddrResolver(n net.Interface) ([]net.Addr, error)
	LookupIP(s string) ([]net.IP, error)
	LdapHandler(ldapHandler carbonio.LocalConfig) carbonio.LdapHandler
	LocalConfigLoader(path string) (carbonio.LocalConfig, error)
	SystemdUnitHandler() (systemd.UnitManager, error)
	CreateCommand(name string, args ...string) exec.Cmd
	GetuidSyscall() int
	LookupUser(name string) (*user.User, error)
	LookupGroup(name string) (*user.Group, error)
	Chown(path string, userUID int, groupUID int) error
	Chmod(path string, mode os.FileMode) error
}

// RealDependencies provides the real implementations of setup dependencies.
type RealDependencies struct {
	UI *term.Terminal
}

// Term returns the terminal interface.
func (r RealDependencies) Term() term.Terminal {
	return *r.UI
}

// Writer returns the standard output writer.
func (r RealDependencies) Writer() io.Writer {
	return os.Stdout
}

// Reader returns the standard input reader.
func (r RealDependencies) Reader() io.Reader {
	return os.Stdin
}

// NetInterfaces returns all network interfaces.
func (r RealDependencies) NetInterfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

// AddrResolver returns the addresses for a network interface.
func (r RealDependencies) AddrResolver(n net.Interface) ([]net.Addr, error) {
	return n.Addrs()
}

// LookupIP resolves a hostname to IP addresses.
func (r RealDependencies) LookupIP(s string) ([]net.IP, error) {
	resolver := &net.Resolver{}

	ipAddrs, err := resolver.LookupIPAddr(context.Background(), s)
	if err != nil {
		return nil, err
	}

	ips := make([]net.IP, len(ipAddrs))
	for i, addr := range ipAddrs {
		ips[i] = addr.IP
	}

	return ips, nil
}

// LdapHandler creates a new LDAP handler from the local config.
func (r RealDependencies) LdapHandler(config carbonio.LocalConfig) carbonio.LdapHandler {
	return carbonio.CreateNewHandler(config)
}

// LocalConfigLoader loads the local configuration from the given path.
func (r RealDependencies) LocalConfigLoader(path string) (carbonio.LocalConfig, error) {
	return carbonio.LoadLocalConfig(path)
}

// SystemdUnitHandler returns a new systemd unit manager.
func (r RealDependencies) SystemdUnitHandler() (systemd.UnitManager, error) {
	return dbus.NewWithContext(context.Background())
}

// CreateCommand creates a new command with the given name and arguments.
func (r RealDependencies) CreateCommand(name string, args ...string) exec.Cmd {
	return exec.Command(name, args...)
}

// GetuidSyscall returns the current user ID.
func (r RealDependencies) GetuidSyscall() int {
	return os.Getuid()
}

// LookupUser looks up a user by username.
func (r RealDependencies) LookupUser(name string) (*user.User, error) {
	return user.Lookup(name)
}

// LookupGroup looks up a group by name.
func (r RealDependencies) LookupGroup(name string) (*user.Group, error) {
	return user.LookupGroup(name)
}

// Chown changes the owner of a file.
func (r RealDependencies) Chown(path string, userUID, groupUID int) error {
	return os.Chown(path, userUID, groupUID)
}

// Chmod changes the permissions of a file.
func (r RealDependencies) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}
