package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"github.com/zextras/service-discover/pkg/carbonio"
)

const (
	consulBinPath = "/usr/bin/consul"
	// Starting from 1000 to avoid conflicts with consul exit codes.
	ExitCodeWrongArgs = 1001
	ExitCodeUserStuff = 1002
	ExitCodeLocalCfg  = 1003
	ExitCodeLdapError = 1004
	ExitCodeExecError = 1005
)

type realDependencies struct{}

func (r realDependencies) Exit(code int) {
	os.Exit(code)
}

func (r realDependencies) Log(a ...any) {
	_, _ = fmt.Fprint(os.Stderr, a...)
}

func (r realDependencies) Getenv(key string) string {
	return os.Getenv(key)
}

func (r realDependencies) Getuid() int {
	return os.Getuid()
}

func (r realDependencies) UserLookup(username string) (*user.User, error) {
	return user.Lookup(username)
}

func (r realDependencies) Setuid(uid int) error {
	return syscall.Setuid(uid)
}

func (r realDependencies) Setgid(gid int) error {
	return syscall.Setgid(gid)
}

func (r realDependencies) Exec(argv0 string, argv, envv []string) error {
	return syscall.Exec(argv0, argv, envv)
}

func (r realDependencies) LoadLocalConfig() (carbonio.LocalConfig, error) {
	localConfig, err := carbonio.LoadLocalConfig(carbonio.LocalConfigPath)

	return localConfig, err
}

func (r realDependencies) CreateNewHandler(localConfig carbonio.LocalConfig) carbonio.LdapHandler {
	return carbonio.CreateNewHandler(localConfig)
}

type deps interface {
	Exit(code int)
	Log(a ...any)
	Getenv(key string) (env string)
	Getuid() (uid int)
	UserLookup(username string) (*user.User, error)
	Setuid(uid int) (err error)
	Setgid(gid int) (err error)
	Exec(argv0 string, argv []string, envv []string) (err error)
	LoadLocalConfig() (carbonio.LocalConfig, error)
	CreateNewHandler(localConfig carbonio.LocalConfig) carbonio.LdapHandler
}

type ErrorWithExitCode struct {
	Log      string
	ExitCode int
}

func main() {
	runServiceDiscoverDaemon(realDependencies{}, os.Args)
}

func runServiceDiscoverDaemon(deps deps, args []string) {
	if len(args) < 2 || (args[1] != "server" && args[1] != "agent") {
		deps.Log("one parameter: server or agent")
		deps.Exit(ExitCodeWrongArgs)

		return
	}

	isServer := args[1] == "server"

	// root privileges only serves to read the localconfig, once we have the
	// necessary credentials we can drop privileges to reduce the attack surface
	err := checkRoot(deps)
	if err != nil {
		deps.Log(err.Log)
		deps.Exit(err.ExitCode)

		return
	}

	ldapHandler, localServer, err := readLocalConfig(deps)
	if err != nil {
		deps.Log(err.Log)
		deps.Exit(err.ExitCode)

		return
	}

	err = changeUser(deps)

	if err != nil {
		deps.Log(err.Log)
		deps.Exit(err.ExitCode)

		return
	}

	servers, err := queryAllServiceDiscoverServers(ldapHandler)
	if err != nil {
		deps.Log(err.Log)
		deps.Exit(err.ExitCode)

		return
	}

	err = startConsul(deps, isServer, servers, localServer)
	if err != nil {
		deps.Log(err.Log)
		deps.Exit(err.ExitCode)

		return
	}

	panic("service-discoverd failure")
}

func checkRoot(d deps) *ErrorWithExitCode {
	uid := d.Getuid()
	if uid != 0 {
		return &ErrorWithExitCode{
			Log:      "run as root",
			ExitCode: ExitCodeUserStuff,
		}
	}

	return nil
}

func changeUser(deps deps) *ErrorWithExitCode {
	serviceDiscoverUser, err := deps.UserLookup("service-discover")
	if err != nil {
		return &ErrorWithExitCode{
			Log:      "cannot find user 'service-discover': " + err.Error(),
			ExitCode: ExitCodeUserStuff,
		}
	}

	uid, err := strconv.Atoi(serviceDiscoverUser.Uid)
	if err != nil {
		return &ErrorWithExitCode{
			Log:      "cannot parse uid: " + err.Error(),
			ExitCode: ExitCodeUserStuff,
		}
	}

	gid, err := strconv.Atoi(serviceDiscoverUser.Gid)
	if err != nil {
		return &ErrorWithExitCode{
			Log:      "cannot parse gid: " + err.Error(),
			ExitCode: ExitCodeUserStuff,
		}
	}

	err = deps.Setgid(gid)
	if err != nil {
		return &ErrorWithExitCode{
			Log:      "cannot change gid: " + err.Error(),
			ExitCode: ExitCodeUserStuff,
		}
	}

	err = deps.Setuid(uid)
	if err != nil {
		return &ErrorWithExitCode{
			Log:      "cannot change uid: " + err.Error(),
			ExitCode: ExitCodeUserStuff,
		}
	}

	return nil
}

func startConsul(deps deps, isServer bool, servers []string, localServer string) *ErrorWithExitCode {
	var args []string

	if isServer {
		args = []string{
			consulBinPath,
			"agent",
			"-bootstrap-expect",
			strconv.Itoa(len(servers)/2 + 1),
			"-config-dir",
			"/etc/zextras/service-discover/",
			"-server",
		}
	} else {
		args = []string{
			consulBinPath,
			"agent",
			"-config-dir",
			"/etc/zextras/service-discover/",
		}
	}

	// HACK: consul doesn't notify readiness to systemd if the list of servers is empty
	if isServer {
		args = append(args, ("-retry-join=" + localServer))
	}

	found := false

	for _, server := range servers {
		if localServer != server {
			args = append(args, "-retry-join="+server)
		} else {
			found = true
		}
	}

	if isServer && !found {
		// a consul server is missing from ldap could cause trouble
		// better to stop it
		return &ErrorWithExitCode{
			Log:      "local service-discover server NOT present in ldap/zimbraServiceEnabled attribute " + localServer,
			ExitCode: ExitCodeLdapError,
		}
	}

	if !isServer && found {
		// consul agent is written in ldap when it shouldn't be
		// better to stop it
		return &ErrorWithExitCode{
			Log:      "local service-discover agent must NOT be present in ldap/zimbraServiceEnabled attribute " + localServer,
			ExitCode: ExitCodeLdapError,
		}
	}

	envs := make([]string, 0)

	if deps.Getenv("SHELL") != "" {
		envs = append(envs, "SHELL="+deps.Getenv("SHELL"))
	}

	if deps.Getenv("NOTIFY_SOCKET") != "" {
		envs = append(envs, "NOTIFY_SOCKET="+deps.Getenv("NOTIFY_SOCKET"))
	}

	err := deps.Exec(
		consulBinPath,
		args,
		envs,
	)
	if err != nil {
		return &ErrorWithExitCode{
			Log:      "consul execute failed: " + err.Error(),
			ExitCode: ExitCodeExecError,
		}
	}

	return &ErrorWithExitCode{
		Log:      "consul execute failed, still running",
		ExitCode: ExitCodeExecError,
	}
}

func readLocalConfig(deps deps) (carbonio.LdapHandler, string, *ErrorWithExitCode) {
	localConfig, err := deps.LoadLocalConfig()
	if err != nil {
		return nil, "", &ErrorWithExitCode{
			Log:      "unable to read ldap configuration: " + err.Error(),
			ExitCode: ExitCodeLocalCfg,
		}
	}

	handler := deps.CreateNewHandler(localConfig)

	return handler, localConfig.Value(carbonio.LocalConfigServerHostname), nil
}

func queryAllServiceDiscoverServers(ldapHandler carbonio.LdapHandler) ([]string, *ErrorWithExitCode) {
	servers, err := ldapHandler.QueryAllServersWithService(carbonio.ServiceDiscoverServiceName)
	if err != nil {
		return nil, &ErrorWithExitCode{
			Log:      "unable to query ldap: " + err.Error(),
			ExitCode: ExitCodeLdapError,
		}
	}

	return servers, nil
}
