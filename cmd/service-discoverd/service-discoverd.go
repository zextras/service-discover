package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/mesh"
	"github.com/zextras/service-discover/pkg/spire"
)

const (
	consulBinPath = "/usr/bin/consul"
	// consulAgentCommand is the consul subcommand used for both server and
	// agent instances.
	consulAgentCommand = "agent"
	serverMode         = "server"
	agentMode          = "agent"
	// ExitCodeWrongArgs and the following codes start from 64: a process exit
	// status is truncated to 8 bits, so codes must stay below 256, and they
	// must avoid the range 200-243 that systemd reserves for its own
	// exit statuses (e.g. 236 is shown as SMACK_PROCESS_LABEL).
	ExitCodeWrongArgs  = 64
	ExitCodeUserStuff  = 65
	ExitCodeLocalCfg   = 66
	ExitCodeLdapError  = 67
	ExitCodeExecError  = 68
	ReadMainJSON       = 69
	ParseMainJSON      = 70
	ParsePortsMainJSON = 71
	MarshallMainJSON   = 72
	WriteMainJSON      = 73

	// The LDAP server may be restarting together with service-discoverd
	// (e.g. systemctl restart carbonio.target): retry the startup query with
	// an exponential backoff for a bounded window before giving up.
	ldapRetryWindow       = 60 * time.Second
	ldapRetryInitialDelay = 1 * time.Second
	ldapRetryMaxDelay     = 5 * time.Second
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

// spireMode and offlineEnv are the node mode and the environment preparing the configuration of Consul, variables for
// the tests.
var (
	spireMode  = mesh.SPIREMode
	offlineEnv = func() *mesh.Env { return mesh.NewOfflineEnv(spire.TrustDomain) }
)

// ConsulConfigArgs returns the configuration arguments of Consul for a node using SPIRE, nil for the others.
func (r realDependencies) ConsulConfigArgs() ([]string, error) {
	if !spireMode() {
		return nil, nil
	}

	defs, err := mesh.Load()
	if err != nil {
		return nil, err
	}

	return mesh.ConsulConfigArgs(offlineEnv(), defs)
}

func (r realDependencies) Now() time.Time {
	return time.Now()
}

func (r realDependencies) Sleep(d time.Duration) {
	time.Sleep(d)
}

type deps interface { //nolint:interfacebloat // the dependencies of the startup
	Exit(code int)
	Now() time.Time
	Sleep(d time.Duration)
	Log(a ...any)
	Getenv(key string) (env string)
	Getuid() (uid int)
	UserLookup(username string) (*user.User, error)
	Setuid(uid int) (err error)
	Setgid(gid int) (err error)
	Exec(argv0 string, argv []string, envv []string) (err error)
	LoadLocalConfig() (carbonio.LocalConfig, error)
	CreateNewHandler(localConfig carbonio.LocalConfig) carbonio.LdapHandler
	ConsulConfigArgs() ([]string, error)
}

type ErrorWithExitCode struct {
	Log      string
	ExitCode int
}

func main() {
	runServiceDiscoverDaemon(realDependencies{}, os.Args)
}

func runServiceDiscoverDaemon(deps deps, args []string) {
	if len(args) < 2 || (args[1] != serverMode && args[1] != agentMode) {
		deps.Log("one parameter: server or agent")
		deps.Exit(ExitCodeWrongArgs)

		return
	}

	isServer := args[1] == serverMode

	// root privileges only serves to read the localconfig, once we have the
	// necessary credentials we can drop privileges to reduce the attack surface
	err := checkRoot(deps)
	if err != nil {
		deps.Log(err.Log)
		deps.Exit(err.ExitCode)

		return
	}

	mainJSONPath := "/etc/zextras/service-discover/main.json"
	if len(args) > 2 {
		mainJSONPath = args[2]
	}

	var (
		ldapHandler carbonio.LdapHandler
		localServer string
		servers     []string
	)

	// An agent joined through SPIRE gets the servers from the auto-config persisted by Consul: it neither needs LDAP
	// nor the localconfig. The configuration is read before dropping privileges, it is not readable otherwise.
	autoConfig := !isServer && autoConfigEnabled(mainJSONPath)

	if !autoConfig {
		ldapHandler, localServer, err = readLocalConfig(deps)
		if err != nil {
			deps.Log(err.Log)
			deps.Exit(err.ExitCode)

			return
		}
	}

	// On a node using SPIRE the service definitions are rendered with the token of each service, which only root can
	// read: Consul loads these instead of the ones of the packages.
	configArgs, cfgErr := deps.ConsulConfigArgs()
	if cfgErr != nil {
		deps.Log("unable to prepare the service definitions: " + cfgErr.Error() + "\n")
	}

	err = changeUser(deps)

	if err != nil {
		deps.Log(err.Log)
		deps.Exit(err.ExitCode)

		return
	}

	if !autoConfig {
		servers, err = queryAllServiceDiscoverServers(deps, ldapHandler)
		if err != nil {
			deps.Log(err.Log)
			deps.Exit(err.ExitCode)

			return
		}
	}

	errTLS := addGrpcTLS(mainJSONPath)
	if errTLS != nil {
		deps.Log(errTLS.Log)
		deps.Exit(errTLS.ExitCode)

		return
	}

	waitForJWKS(deps, mainJSONPath, jwksWaitWindow, jwksPollInterval)

	err = startConsul(deps, isServer, servers, localServer, configArgs)
	if err != nil {
		deps.Log(err.Log)
		deps.Exit(err.ExitCode)

		return
	}

	panic("service-discoverd failure")
}

// autoConfigEnabled reports whether the Consul configuration enables auto_config. A configuration that cannot be read
// or parsed is not an auto_config one: the regular startup reports the problem.
func autoConfigEnabled(configPath string) bool {
	data, err := os.ReadFile(filepath.Clean(configPath)) //nolint:gosec // path of the service configuration
	if err != nil {
		return false
	}

	var cfg struct {
		AutoConfig struct {
			Enabled bool `json:"enabled"`
		} `json:"auto_config"`
	}

	err = json.Unmarshal(data, &cfg)

	return err == nil && cfg.AutoConfig.Enabled
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

// startConsul replaces this process with Consul. configArgs are the arguments loading its configuration, by default
// the whole configuration directory.
func startConsul(
	deps deps, isServer bool, servers []string, localServer string, configArgs []string,
) *ErrorWithExitCode {
	if configArgs == nil {
		configArgs = []string{"-config-dir", "/etc/zextras/service-discover/"}
	}

	args := []string{consulBinPath, consulAgentCommand}

	if isServer {
		args = append(args, "-bootstrap-expect", strconv.Itoa(len(servers)/2+1))
	}

	args = append(args, configArgs...)

	if isServer {
		args = append(args, "-server")
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

// queryAllServiceDiscoverServers queries ldap for the service-discover servers,
// retrying failures with an exponential backoff (ldapRetryInitialDelay doubling,
// capped at ldapRetryMaxDelay) until ldapRetryWindow has elapsed.
func queryAllServiceDiscoverServers(deps deps, ldapHandler carbonio.LdapHandler) ([]string, *ErrorWithExitCode) {
	deadline := deps.Now().Add(ldapRetryWindow)
	delay := ldapRetryInitialDelay

	for attempt := 1; ; attempt++ {
		servers, err := ldapHandler.QueryAllServersWithService(carbonio.ServiceDiscoverServiceName)
		if err == nil {
			return servers, nil
		}

		remaining := deadline.Sub(deps.Now())
		if remaining <= 0 {
			return nil, &ErrorWithExitCode{
				Log:      "unable to query ldap: " + err.Error(),
				ExitCode: ExitCodeLdapError,
			}
		}

		wait := min(delay, remaining)

		deps.Log(fmt.Sprintf("ldap query attempt %d failed: %s; retrying in %s\n", attempt, err, wait))
		deps.Sleep(wait)

		delay = min(delay*2, ldapRetryMaxDelay)
	}
}

func addGrpcTLS(inputFile string) *ErrorWithExitCode {
	// Read the input JSON file
	inputData, err := os.ReadFile(filepath.Clean(inputFile))
	if err != nil {
		return &ErrorWithExitCode{
			Log:      "Failed to read input file:" + err.Error(),
			ExitCode: ReadMainJSON,
		}
	}

	// Parse the JSON data
	var jsonData map[string]any

	err = json.Unmarshal(inputData, &jsonData)
	if err != nil {
		return &ErrorWithExitCode{
			Log:      "Failed to parse JSON data:" + err.Error(),
			ExitCode: ParseMainJSON,
		}
	}

	// Check if the 'ports' field exists and is a map
	ports, ok := jsonData["ports"].(map[string]any)
	if !ok {
		return &ErrorWithExitCode{
			Log:      "Invalid JSON structure: 'ports' field is missing or not an object",
			ExitCode: ParsePortsMainJSON,
		}
	}

	// Check if 'grpc_tls' port does not exist and add it
	if _, exists := ports["grpc_tls"]; !exists {
		ports["grpc_tls"] = 8503

		modifiedData, err := json.MarshalIndent(jsonData, "", "  ")
		if err != nil {
			return &ErrorWithExitCode{
				Log:      "Failed to marshal modified JSON data:" + err.Error(),
				ExitCode: MarshallMainJSON,
			}
		}

		// Write the modified JSON data to the output file
		outputFile := inputFile

		err = os.WriteFile(outputFile, modifiedData, 0644) //nolint:gosec // consul config needs to be readable
		if err != nil {
			return &ErrorWithExitCode{
				Log:      "Failed to write output file:" + err.Error(),
				ExitCode: WriteMainJSON,
			}
		}
	} else {
		fmt.Println("'grpc_tls' port already exists")
	}

	return nil
}
