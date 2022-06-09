package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/exec"
	"bitbucket.org/zextras/service-discover/cli/lib/systemd"
	"bitbucket.org/zextras/service-discover/cli/lib/term"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"context"
	"github.com/coreos/go-systemd/v22/dbus"
	"io"
	"net"
	"os"
	"os/user"
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
	LdapHandler(zimbra.LocalConfig) zimbra.LdapHandler
	LocalConfigLoader(path string) (zimbra.LocalConfig, error)
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

func (r realDependencies) LdapHandler(config zimbra.LocalConfig) zimbra.LdapHandler {
	return zimbra.CreateNewHandler(config)
}

func (r realDependencies) LocalConfigLoader(path string) (zimbra.LocalConfig, error) {
	return zimbra.LoadLocalConfig(path)
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
