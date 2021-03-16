package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bitbucket.org/zextras/service-discover/cli/server/exec"
	"bitbucket.org/zextras/service-discover/cli/server/systemd"
	"context"
	"github.com/coreos/go-systemd/v22/dbus"
	"io"
	"net"
	"os"
)

type interactiveDependencies interface {
	Writer() io.Writer
	Reader() io.Reader
	NetInterfaces() ([]net.Interface, error)
	AddrResolver(n net.Interface) ([]net.Addr, error)
}

type businessDependencies interface {
	Writer() io.Writer
	NetInterfaces() ([]net.Interface, error)
	AddrResolver(n net.Interface) ([]net.Addr, error)
	LdapHandler(zimbra.LocalConfig) zimbra.LdapHandler
	LocalConfigLoader(path string) (zimbra.LocalConfig, error)
	SystemdUnitHandler() (systemd.UnitManager, error)
	CreateCommand(name string, args ...string) exec.Cmd
	GetuidSyscall() int
}

type allDependencies interface {
	interactiveDependencies
	businessDependencies
}

type realDependencies struct {}

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