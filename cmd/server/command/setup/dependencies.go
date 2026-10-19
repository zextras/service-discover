// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/hashicorp/consul/api/v2"
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

type businessDependencies interface { //nolint:interfacebloat // setup requires many system deps
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
	ConsulLeader() (string, error)
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
	return net.LookupIP(s) //nolint:noctx // changing interface signature is too invasive
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

func (r realDependencies) Chown(path string, userUID, groupUID int) error {
	return os.Chown(path, userUID, groupUID)
}

func (r realDependencies) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

// consulLeaderRequestTimeout bounds a single leader lookup, so a hung request cannot outlive the overall wait.
const consulLeaderRequestTimeout = 5 * time.Second

// ConsulLeader returns the address of the current Raft leader as reported by the local Consul agent, or an empty
// string if no leader has been elected yet. It is called in a polling loop, so it uses a non-pooled client that does
// not keep idle connections around between calls.
func (r realDependencies) ConsulLeader() (string, error) {
	config := api.DefaultNonPooledConfig()
	config.HttpClient = &http.Client{Transport: config.Transport, Timeout: consulLeaderRequestTimeout}

	client, err := api.NewClient(config)
	if err != nil {
		return "", err
	}

	return client.Status().Leader()
}
