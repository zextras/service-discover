// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/pkg/errors"
	"github.com/zextras/service-discover/cmd/agent/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/systemd"
	"github.com/zextras/service-discover/pkg/term"
)

var testingMode bool

const (
	rootUID               = 0
	certificateExpiration = 365 * 30
	serviceDiscoverUnit   = "service-discover.service"
	defaultLogLevel       = "INFO"
)

func New() Setup {
	return Setup{
		ConsulConfigDir:   config.ConsulHome,
		ConsulHome:        config.ConsulHome,
		LocalConfigPath:   config.LocalConfigPath,
		ConsulData:        config.ConsulData,
		ConsulFileConfig:  config.ConsultFileConfig,
		ClusterCredential: config.ClusterCredential,
		MutableConfigFile: command.ConsulMutableConfigFile,
	}
}

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

type realDependencies struct {
	ui *term.Terminal
}

func (r realDependencies) Term() term.Terminal {
	return *r.ui
}

func (r realDependencies) NetInterfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

func (r realDependencies) AddrResolver(n net.Interface) ([]net.Addr, error) {
	return n.Addrs()
}

func (r realDependencies) LookupIP(s string) ([]net.IP, error) {
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

func (r realDependencies) LdapHandler(localConfig carbonio.LocalConfig) carbonio.LdapHandler {
	return carbonio.CreateNewHandler(localConfig)
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

type setupConfiguration struct {
	Password    string
	BindAddress string
}

type aclConfig struct {
	Enabled                bool   `json:"enabled"`
	DefaultPolicy          string `json:"default_policy"`
	EnableTokenPersistence bool   `json:"enable_token_persistence"`
	DownPolicy             string `json:"down_policy"`
}

type uiConfig struct {
	Enabled bool `json:"enabled"`
}

type portsConfig struct {
	Grpc    int `json:"grpc"`
	GrpcTLS int `json:"grpc_tls"`
}

type tlsDefaults struct {
	CaFile         string `json:"ca_file"`
	CertFile       string `json:"cert_file"`
	KeyFile        string `json:"key_file"`
	VerifyIncoming bool   `json:"verify_incoming"`
	VerifyOutgoing bool   `json:"verify_outgoing"`
}

type tlsInternalRPC struct {
	VerifyServerHostname bool `json:"verify_server_hostname"`
}

type tlsConfig struct {
	Defaults    tlsDefaults    `json:"defaults"`
	InternalRPC tlsInternalRPC `json:"internal_rpc"`
}

type setupConfig struct {
	ACLConfig               aclConfig   `json:"acl"`
	DataDir                 string      `json:"data_dir"`
	EnableLocalScriptChecks bool        `json:"enable_local_script_checks"`
	Encrypt                 string      `json:"encrypt"`
	LogLevel                string      `json:"log_level"`
	NodeName                string      `json:"node_name"`
	Server                  bool        `json:"server"`
	UIConfig                uiConfig    `json:"ui_config"`
	Ports                   portsConfig `json:"ports"`
	TLS                     tlsConfig   `json:"tls"`
}

type Setup struct {
	ConsulConfigDir   string `kong:"-"`
	ConsulHome        string `kong:"-"`
	LocalConfigPath   string `kong:"-"`
	ConsulData        string `kong:"-"`
	ConsulFileConfig  string `kong:"-"`
	ClusterCredential string `kong:"-"`
	MutableConfigFile string `kong:"-"`

	Wizard bool `help:"Initialize in interactive mode. Non-interactive flags will be ignored"`

	Password    string `help:"Custom password for encrypted secret files. If unset, one is generated"`
	BindAddress string `arg:"" optional:"" help:"The binding address to bind service-discoverd daemon"`
}

func gatherInputs(deps interactiveDependencies) (*setupConfiguration, error) {
	networks, err := deps.NetInterfaces()
	if err != nil {
		return nil, err
	}

	for i, n := range networks {
		if strings.EqualFold(n.Name, "lo") {
			networks[i] = networks[len(networks)-1]
			networks = networks[:len(networks)-1]
		}
	}

	if len(networks) > 1 {
		term.MustWrite(fmt.Fprint(deps.Term(), "Multiple network cards detected:"+term.LineBreak))
	}

	for _, net := range networks {
		addrs, err := deps.AddrResolver(net)
		if err != nil {
			return nil, err
		}

		term.MustWrite(fmt.Fprintf(
			deps.Term(),
			"%s %s%s",
			net.Name,
			command.AddrsToSingleString(&addrs, ", "),
			term.LineBreak,
		))
	}

	term.MustWrite(fmt.Fprint(deps.Term(), "Specify the binding address for service discovery: "))
	bindingAddress := term.MustRead(deps.Term().ReadLine())

	err = command.CheckValidBindingAddress(deps, networks, bindingAddress)
	if err != nil {
		return nil, err
	}

	pass, err := deps.Term().ReadPassword("Insert the cluster credential password: ")
	if err != nil {
		{
			var errCase0 term.NotATerminalError
			switch {
			case errors.As(err, &errCase0):
				pass = term.MustRead(deps.Term().ReadLine())
			default:
				return nil, err
			}
		}
	}

	return &setupConfiguration{
		Password:    pass,
		BindAddress: bindingAddress,
	}, nil
}

func preRun(_ string, deps businessDependencies) error {
	// We need to check that the executable is in $PATH
	cmd := deps.CreateCommand(command.ConsulBin, "version")

	err := cmd.Run()
	if err != nil {
		return errors.Errorf("unable to execute consul binary: %s", err)
	}

	if deps.GetuidSyscall() != rootUID {
		return errors.New("this command must be executed as root")
	}

	_, err = os.Stat(config.ConsultFileConfig)
	if err == nil {
		return errors.New("setup of service-discover already performed, manually reset and try again")
	}

	return nil
}

// saveBindAddressWithPermissions saves the bind address configuration and sets strict permissions.
// This combines two operations that always appear together in the setup workflow.
func saveBindAddressWithPermissions(deps businessDependencies, configPath, bindAddress string) error {
	err := command.SaveBindAddressConfiguration(configPath, bindAddress)
	if err != nil {
		return err
	}

	return permissions.SetStrictPermissions(deps, configPath)
}

func (s *Setup) Run(commonFlags *command.GlobalCommonFlags) error {
	userInterface, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return err
	}

	defer userInterface.Close()

	deps := realDependencies{
		ui: &userInterface,
	}

	err = preRun(s.ClusterCredential, &deps)
	if err != nil {
		return err
	}

	if s.Password == "" && s.BindAddress == "" {
		return errors.New("missing arguments")
	}

	out, err := s.setup(&deps)
	if err != nil {
		return err
	}

	if !s.Wizard {
		render, err := formatter.Render(out, commonFlags.Format)
		if err != nil {
			return err
		}

		term.MustWrite(deps.Term().WriteString(render))
	}

	return nil
}

func (s *Setup) createTLSCertificate(deps businessDependencies, caFile, caKeyFile *os.File) error {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)

	err := exec.InPath(
		// FIXME idea: what if we try to pass the caFile by pipe instead of passing a file?
		// we save I/O and speed up the whole stuff 🤙
		deps.CreateCommand(command.ConsulBin,
			"tls",
			"cert",
			"create",
			certificateDaysFlag,
			"-ca",
			caFile.Name(),
			"-key",
			caKeyFile.Name(),
			"-client"),
		s.ConsulHome,
	)
	if err != nil {
		return exec.ErrorFromStderr(err, "unable to generate correct CA certificate")
	}

	err = permissions.SetStrictPermissions(deps, filepath.Join(s.ConsulHome, command.ConsulAgentCertificate))
	if err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(deps, filepath.Join(s.ConsulHome, command.ConsulAgentCertificateKey))
	if err != nil {
		return err
	}

	return nil
}

//nolint:misspell
func (s *Setup) setup(deps businessDependencies) (formatter.Formatter, error) {
	zimbraHostname, _, err := s.setupNetworkAndConfig(deps)
	if err != nil {
		return nil, err
	}

	extractedFiles, err := s.extractCredentialsFromArchive()
	if err != nil {
		return nil, err
	}

	err = s.setupCertificates(deps, extractedFiles)
	if err != nil {
		return nil, err
	}

	err = command.CheckHostnameAddress(deps, zimbraHostname)
	if err != nil {
		return nil, err
	}

	err = s.writeConsulConfig(deps, extractedFiles, zimbraHostname)
	if err != nil {
		return nil, err
	}

	isContainer, err := s.startServiceDiscover(deps)
	if err != nil {
		return nil, err
	}

	err = s.setupACLTokens(deps, extractedFiles, zimbraHostname, isContainer)
	if err != nil {
		return nil, err
	}

	return &formatter.EmptyFormatter{}, nil
}

func (s *Setup) setupNetworkAndConfig(deps businessDependencies) (string, carbonio.LdapHandler, error) {
	networks, err := command.NonLoopbackInterfaces(deps)
	if err != nil {
		return "", nil, err
	}

	err = command.CheckValidBindingAddress(deps, networks, s.BindAddress)
	if err != nil {
		return "", nil, err
	}

	zimbraLocalConfig, err := carbonio.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return "", nil, err
	}

	ldapHandler := deps.LdapHandler(zimbraLocalConfig)

	zimbraHostname, err := command.RetrieveZimbraHostname(zimbraLocalConfig, ldapHandler)
	if err != nil {
		return "", nil, err
	}

	err = command.DownloadCredentialsFromLDAP(ldapHandler, s.ClusterCredential)
	if err != nil {
		return "", nil, errors.WithMessage(err, "unable to download credentials from LDAP")
	}

	return zimbraHostname, ldapHandler, nil
}

func (s *Setup) extractCredentialsFromArchive() (map[string][]byte, error) {
	clusterCredentialFile, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return nil, errors.Errorf("unable to open %s: %s", s.ClusterCredential, err)
	}

	defer func(clusterCredentialFile *os.File) {
		_ = clusterCredentialFile.Close()
	}(clusterCredentialFile)

	credReader, err := encrypter.NewReader(clusterCredentialFile, []byte(s.Password))
	if err != nil {
		return nil, errors.WithMessagef(err, "unable to read %s", clusterCredentialFile.Name())
	}

	// We calculate the path relative to the root (i.e. without the "/" at the beginning) since this should not be
	// included in standard tarballs
	caPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCA))
	if err != nil {
		return nil, err
	}

	caKeyPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCAKey))
	if err != nil {
		return nil, err
	}

	extractedFiles, err := encrypter.ReadFiles(credReader,
		caPath, caKeyPath, command.GossipKey, command.ConsulACLBootstrap)
	if err != nil {
		return nil, err
	}

	return extractedFiles, nil
}

func (s *Setup) setupCertificates(deps businessDependencies, extractedFiles map[string][]byte) error {
	caPath, _ := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCA))
	caKeyPath, _ := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCAKey))

	caFile, err := os.Create(s.ConsulHome + "/" + command.ConsulCA)
	if err != nil {
		return err
	}

	err = os.WriteFile(caFile.Name(), extractedFiles[caPath], os.FileMode(0600))
	if err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(deps, caFile.Name())
	if err != nil {
		return err
	}

	caKeyFile, err := os.CreateTemp("", config.ApplicationName+"*")
	if err != nil {
		return err
	}

	defer os.Remove(caKeyFile.Name())

	err = os.WriteFile(caKeyFile.Name(), extractedFiles[caKeyPath], os.FileMode(0600))
	if err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(deps, caKeyFile.Name())
	if err != nil {
		return err
	}

	err = s.createTLSCertificate(deps, caFile, caKeyFile)
	if err != nil {
		return err
	}

	err = os.Remove(caKeyFile.Name())
	if err != nil {
		return errors.WithMessage(err, "cannot remove secret "+caKeyFile.Name()+" please remove it manually")
	}

	return nil
}

func (s *Setup) writeConsulConfig(
	deps businessDependencies,
	extractedFiles map[string][]byte,
	zimbraHostname string,
) error {
	consulAgentConfig := &setupConfig{
		ACLConfig: aclConfig{
			Enabled:                true,
			DefaultPolicy:          "deny",
			DownPolicy:             "extend-cache",
			EnableTokenPersistence: true,
		},
		DataDir:                 s.ConsulData,
		EnableLocalScriptChecks: true,
		Encrypt:                 string(extractedFiles[command.GossipKey]),
		LogLevel:                defaultLogLevel,
		NodeName:                command.ConsulNodeName(command.Agent, zimbraHostname),
		Server:                  false,
		UIConfig: uiConfig{
			Enabled: true,
		},
		Ports: portsConfig{
			Grpc:    8502,
			GrpcTLS: 8503,
		},
		TLS: tlsConfig{
			Defaults: tlsDefaults{
				CaFile:         s.ConsulHome + "/" + command.ConsulCA,
				CertFile:       s.ConsulHome + "/" + command.ConsulAgentCertificate,
				KeyFile:        s.ConsulHome + "/" + command.ConsulAgentCertificateKey,
				VerifyIncoming: true,
				VerifyOutgoing: true,
			},
			InternalRPC: tlsInternalRPC{
				VerifyServerHostname: true,
			},
		},
	}

	err := writeSetupConfig(consulAgentConfig, s.ConsulFileConfig)
	if err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(deps, s.ConsulFileConfig)
	if err != nil {
		return err
	}

	return saveBindAddressWithPermissions(deps, s.MutableConfigFile, s.BindAddress)
}

func (s *Setup) startServiceDiscover(deps businessDependencies) (bool, error) {
	return startServiceDiscoverMode(deps, "agent")
}

// startServiceDiscoverMode starts service-discover in the specified mode (agent or server).
// It handles both container and systemd environments.
func startServiceDiscoverMode(deps businessDependencies, mode string) (bool, error) {
	isContainer := command.CheckDockerContainer()
	if isContainer && !testingMode {
		cmd := exec.Command("service-discoverd-docker", mode)

		err := cmd.Run()
		if err != nil {
			return isContainer, errors.WithMessagef(err, "unable to start service-discoverd")
		}
	} else {
		err := systemd.StartSystemdUnit(deps.SystemdUnitHandler, serviceDiscoverUnit)
		if err != nil {
			return isContainer, errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
		}
	}

	return isContainer, nil
}

func (s *Setup) setupACLTokens(
	deps businessDependencies,
	extractedFiles map[string][]byte,
	zimbraHostname string,
	isContainer bool,
) error {
	aclBootstrapToken := command.ACLTokenCreation{}

	err := json.Unmarshal(extractedFiles[command.ConsulACLBootstrap], &aclBootstrapToken)
	if err != nil {
		return errors.WithMessagef(err, "unable to decode ACL Bootstrap token")
	}

	token, err := command.CreateACLToken(deps.CreateCommand, command.Agent, zimbraHostname, aclBootstrapToken.SecretID)
	if err != nil {
		return errors.WithMessage(err, "unable to create ACL policy for this agent")
	}

	err = command.SetACLToken(deps.CreateCommand, token, aclBootstrapToken.SecretID)
	if err != nil {
		return err
	}

	if !isContainer || testingMode {
		err = systemd.EnableSystemdUnit(deps.SystemdUnitHandler, serviceDiscoverUnit)
		if err != nil {
			return errors.Errorf("unable to enable %s unit: %s", serviceDiscoverUnit, err)
		}
	}

	return nil
}

func writeSetupConfig(consulAgentConfig *setupConfig, destination string) error {
	consulAgentBs, err := json.MarshalIndent(consulAgentConfig, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(destination, consulAgentBs, os.FileMode(0600))
	if err != nil {
		return errors.WithMessagef(err, "unable to save generated configuration file in %s", destination)
	}

	return nil
}
