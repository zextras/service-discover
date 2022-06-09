package setup

import (
	"bitbucket.org/zextras/service-discover/cli/agent/config"
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	"bitbucket.org/zextras/service-discover/cli/lib/exec"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/permissions"
	"bitbucket.org/zextras/service-discover/cli/lib/systemd"
	"bitbucket.org/zextras/service-discover/cli/lib/term"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"context"
	"encoding/json"
	"fmt"
	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/pkg/errors"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

const (
	rootUid               = 0
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
	Grpc int `json:"grpc"`
}

type setupConfig struct {
	AclConfig               aclConfig   `json:"acl"`
	CaFile                  string      `json:"ca_file"`
	CertFile                string      `json:"cert_file"`
	DataDir                 string      `json:"data_dir"`
	EnableLocalScriptChecks bool        `json:"enable_local_script_checks"`
	Encrypt                 string      `json:"encrypt"`
	KeyFile                 string      `json:"key_file"`
	LogLevel                string      `json:"log_level"`
	NodeName                string      `json:"node_name"`
	Server                  bool        `json:"server"`
	VerifyIncoming          bool        `json:"verify_incoming"`
	VerifyOutgoing          bool        `json:"verify_outgoing"`
	VerifyServerHostname    bool        `json:"verify_server_hostname"`
	UiConfig                uiConfig    `json:"ui_config"`
	Ports                   portsConfig `json:"ports"`
}

type Setup struct {
	ConsulConfigDir   string `kong:"-"`
	ConsulHome        string `kong:"-"`
	LocalConfigPath   string `kong:"-"`
	ConsulData        string `kong:"-"`
	ConsulFileConfig  string `kong:"-"`
	ClusterCredential string `kong:"-"`
	MutableConfigFile string `kong:"-"`

	Wizard bool `help:"Initialize the setup in interactive mode. All the non interactive flags will be ignored if this is set"`

	Password    string `help:"Set a custom password for the encrypted secret files. If none is set, a random one will be generated and printed"`
	BindAddress string `arg optional help:"The binding address to bind service-discoverd daemon"`
}

func gatherInputs(d interactiveDependencies) (*setupConfiguration, error) {
	networks, err := d.NetInterfaces()
	if err != nil {
		return nil, err
	}

	for i, n := range networks {
		if strings.ToLower(n.Name) == "lo" {
			networks[i] = networks[len(networks)-1]
			networks = networks[:len(networks)-1]
		}
	}

	if len(networks) > 1 {
		term.MustWrite(fmt.Fprint(d.Term(), "Multiple network cards detected:"+term.LineBreak))
	}

	for _, n := range networks {
		addrs, err := d.AddrResolver(n)
		if err != nil {
			return nil, err
		}

		term.MustWrite(fmt.Fprintf(
			d.Term(),
			"%s %s%s",
			n.Name,
			command.AddrsToSingleString(&addrs, ", "),
			term.LineBreak,
		))
	}

	term.MustWrite(fmt.Fprint(d.Term(), "Specify the binding address for service discovery: "))
	bindingAddress := term.MustRead(d.Term().ReadLine())
	err = command.CheckValidBindingAddress(d, networks, bindingAddress)
	if err != nil {
		return nil, err
	}

	pass, err := d.Term().ReadPassword("Insert the cluster credential password: ")
	if err != nil {
		switch err.(type) {
		case term.NotATerminalError:
			pass = term.MustRead(d.Term().ReadLine())
		default:
			return nil, err
		}
	}

	return &setupConfiguration{
		Password:    pass,
		BindAddress: bindingAddress,
	}, nil
}

func preRun(clusterCredentialPath string, d businessDependencies) error {
	// We need to check that the executable is in $PATH
	cmd := d.CreateCommand(command.ConsulBin, "version")
	err := cmd.Run()
	if err != nil {
		return errors.New(fmt.Sprintf("unable to execute consul binary: %s", err))
	}

	if d.GetuidSyscall() != rootUid {
		return errors.New("this command must be executed as root")
	}

	clusterCredentialFile, err := command.OpenClusterCredential(clusterCredentialPath)
	if err != nil {
		return err
	}
	defer clusterCredentialFile.Close()

	_, err = os.Stat(config.ConsultFileConfig)
	if err == nil {
		return errors.New(fmt.Sprintf("setup of service-discover already perfomed, manually reset and try again."))
	}

	return nil
}

func (s *Setup) Run(commonFlags *command.GlobalCommonFlags) error {
	ui, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return err
	}
	defer ui.Close()
	d := realDependencies{
		ui: &ui,
	}

	err = preRun(s.ClusterCredential, &d)
	if err != nil {
		return err
	}

	if s.Password == "" && s.BindAddress == "" {
		return errors.New("missing arguments")
	}

	out, err := s.setup(&d)
	if err != nil {
		return err
	}

	if !s.Wizard {
		render, err := formatter.Render(out, commonFlags.Format)
		if err != nil {
			return err
		}
		term.MustWrite(d.Term().WriteString(render))
	}
	return nil
}

func (s *Setup) createTLSCertificate(d businessDependencies, caFile *os.File, caKeyFile *os.File) error {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
	err := exec.InPath(
		// FIXME idea: what if we try to pass the caFile by pipe instead of passing a file?
		// we save I/O and speed up the whole stuff 🤙
		d.CreateCommand(command.ConsulBin,
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

	err = permissions.SetStrictPermissions(d, filepath.Join(s.ConsulHome, command.ConsulAgentCertificate))
	if err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(d, filepath.Join(s.ConsulHome, command.ConsulAgentCertificateKey))
	if err != nil {
		return err
	}
	return nil
}

func (s *Setup) setup(d businessDependencies) (formatter.Formatter, error) {
	networks, err := command.NonLoopbackInterfaces(d)
	if err != nil {
		return nil, err
	}
	if err := command.CheckValidBindingAddress(d, networks, s.BindAddress); err != nil {
		return nil, err
	}
	clusterCredentialFile, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("unable to open %s: %s", s.ClusterCredential, err))
	}
	defer clusterCredentialFile.Close()
	credReader, err := credentialsEncrypter.NewReader(clusterCredentialFile, []byte(s.Password))
	if err != nil {
		return nil, err
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
	extractedFiles, err := credentialsEncrypter.ReadFiles(credReader, caPath, caKeyPath, command.GossipKey, command.ConsulAclBootstrap)
	if err != nil {
		return nil, err
	}
	caFile, err := os.Create(s.ConsulHome + "/" + command.ConsulCA)
	if err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(caFile.Name(), extractedFiles[caPath], os.FileMode(0600)); err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(d, caFile.Name())
	if err != nil {
		return nil, err
	}

	caKeyFile, err := ioutil.TempFile("", config.ApplicationName+"*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(caKeyFile.Name())
	if err := ioutil.WriteFile(caKeyFile.Name(), extractedFiles[caKeyPath], os.FileMode(0600)); err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(d, caKeyFile.Name())
	if err != nil {
		return nil, err
	}

	if err := s.createTLSCertificate(d, caFile, caKeyFile); err != nil {
		return nil, err
	}

	if err := os.Remove(caKeyFile.Name()); err != nil {
		return nil, errors.WithMessage(err, "cannot remove secret "+caKeyFile.Name()+" please remove it manually")
	}

	zimbraLocalConfig, err := zimbra.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return nil, err
	}
	ldapHandler := d.LdapHandler(zimbraLocalConfig)
	zimbraHostname, err := command.RetrieveZimbraHostname(zimbraLocalConfig, ldapHandler)
	if err != nil {
		return nil, err
	}

	err = command.CheckHostnameAddress(d, zimbraHostname)
	if err != nil {
		return nil, err
	}

	consulAgentConfig := &setupConfig{
		AclConfig: aclConfig{
			Enabled:                true,
			DefaultPolicy:          "deny",
			DownPolicy:             "extend-cache",
			EnableTokenPersistence: true,
		},
		CaFile:                  s.ConsulHome + "/" + command.ConsulCA,
		CertFile:                s.ConsulHome + "/" + command.ConsulAgentCertificate,
		DataDir:                 s.ConsulData,
		EnableLocalScriptChecks: true,
		Encrypt:                 string(extractedFiles[command.GossipKey]),
		KeyFile:                 s.ConsulHome + "/" + command.ConsulAgentCertificateKey,
		LogLevel:                defaultLogLevel,
		NodeName:                command.ConsulNodeName(command.Agent, zimbraHostname),
		Server:                  false,
		VerifyIncoming:          true,
		VerifyOutgoing:          true,
		VerifyServerHostname:    true,
		UiConfig: uiConfig{
			Enabled: true,
		},
		Ports: portsConfig{
			Grpc: 8502,
		},
	}

	if err := writeSetupConfig(consulAgentConfig, s.ConsulFileConfig); err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(d, s.ConsulFileConfig)
	if err != nil {
		return nil, err
	}

	if err := command.SaveBindAddressConfiguration(s.MutableConfigFile, s.BindAddress); err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(d, s.MutableConfigFile)
	if err != nil {
		return nil, err
	}

	if err := systemd.StartSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit); err != nil {
		return nil, errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
	}
	aclBootstrapToken := command.ACLTokenCreation{}
	if err := json.Unmarshal(extractedFiles[command.ConsulAclBootstrap], &aclBootstrapToken); err != nil {
		return nil, errors.WithMessagef(err, "unable to decode ACL Bootstrap token")
	}

	token, err := command.CreateACLToken(d.CreateCommand, command.Agent, zimbraHostname, aclBootstrapToken.SecretID)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to create ACL policy for this agent")
	}
	err = command.SetACLToken(d.CreateCommand, token, aclBootstrapToken.SecretID)
	if err != nil {
		return nil, err
	}

	err = systemd.EnableSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("unable to enable %s unit: %s", serviceDiscoverUnit, err))
	}

	return &formatter.EmptyFormatter{}, nil
}

func writeSetupConfig(consulAgentConfig *setupConfig, destination string) error {
	consulAgentBs, err := json.MarshalIndent(consulAgentConfig, "", "  ")
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(destination, consulAgentBs, os.FileMode(0600)); err != nil {
		return errors.WithMessagef(err, "unable to save generated configuration file in %s", destination)
	}

	return err
}
