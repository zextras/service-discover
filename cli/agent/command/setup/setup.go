package setup

import (
	"bitbucket.org/zextras/service-discover/cli/agent/config"
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/command/setup"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	"bitbucket.org/zextras/service-discover/cli/lib/exec"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
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
}

type businessDependencies interface {
	NetInterfaces() ([]net.Interface, error)
	AddrResolver(n net.Interface) ([]net.Addr, error)
	LdapHandler(zimbra.LocalConfig) zimbra.LdapHandler
	LocalConfigLoader(path string) (zimbra.LocalConfig, error)
	SystemdUnitHandler() (systemd.UnitManager, error)
	CreateCommand(name string, args ...string) exec.Cmd
	GetuidSyscall() int
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
			setup.AddrsToSingleString(&addrs, ", "),
			term.LineBreak,
		))
	}

	term.MustWrite(fmt.Fprint(d.Term(), "Specify the binding address for service discovery: "))
	bindingAddress := term.MustRead(d.Term().ReadLine())
	err = setup.CheckValidBindingAddress(d, networks, bindingAddress)
	if err != nil {
		return nil, err
	}

	pass, err := d.Term().ReadPassword("Insert the cluster credential password: ")
	if err != nil {
		if ok := err.(*term.NotATerminalError); ok != nil {
			pass = term.MustRead(d.Term().ReadLine())
		} else {
			return nil, err
		}
	}

	return &setupConfiguration{
		Password:    pass,
		BindAddress: bindingAddress,
	}, nil
}

func (s *Setup) preRun(d businessDependencies) error {
	// We need to check that the executable is in $PATH
	cmd := d.CreateCommand(setup.ConsulBin, "version")
	err := cmd.Run()
	if err != nil {
		return errors.New(fmt.Sprintf("unable to execute consul binary: %s", err))
	}

	if d.GetuidSyscall() != rootUid {
		return errors.New("this command must be executed as root")
	}

	clusterCredentialFile, err := setup.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return err
	}
	defer clusterCredentialFile.Close()

	//TODO: check if already present in LDAP or second setup

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

	err = s.preRun(&d)
	if err != nil {
		return err
	}

	if s.Wizard {
		if commonFlags.Format != formatter.PlainFormatOutput {
			return errors.New("only plain formatting is supported when in wizard mode")
		}
		inputs, err := gatherInputs(d)
		if err != nil {
			return err
		}

		s.Password = inputs.Password
		s.BindAddress = inputs.BindAddress
	} else {
		if s.Password == "" && s.BindAddress == "" {
			return errors.New("missing arguments")
		}
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
		d.CreateCommand(setup.ConsulBin,
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
	return nil
}

func (s *Setup) setup(d businessDependencies) (formatter.Formatter, error) {
	networks, err := setup.NonLoopbackInterfaces(d)
	if err != nil {
		return nil, err
	}
	if err := setup.CheckValidBindingAddress(d, networks, s.BindAddress); err != nil {
		return nil, err
	}
	clusterCredentialFile, err := setup.OpenClusterCredential(s.ClusterCredential)
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
	caPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, setup.ConsulCA))
	if err != nil {
		return nil, err
	}
	caKeyPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, setup.ConsulCAKey))
	if err != nil {
		return nil, err
	}
	extractedFiles, err := credentialsEncrypter.ReadFiles(credReader, caPath, caKeyPath, setup.GossipKey, setup.ConsulAclBootstrap)
	if err != nil {
		return nil, err
	}
	caFile, err := os.Create(s.ConsulHome + "/" + setup.ConsulCA)
	if err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(caFile.Name(), extractedFiles[caPath], os.FileMode(0644)); err != nil {
		return nil, err
	}

	caKeyFile, err := ioutil.TempFile("", config.ApplicationName+"*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(caKeyFile.Name())
	if err := ioutil.WriteFile(caKeyFile.Name(), extractedFiles[caKeyPath], os.FileMode(0644)); err != nil {
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
	zimbraHostname, err := setup.RetrieveZimbraHostname(zimbraLocalConfig, ldapHandler)
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
		CaFile:                  s.ConsulHome + "/" + setup.ConsulCA,
		CertFile:                s.ConsulHome + "/" + setup.ConsulAgentCertificate,
		DataDir:                 s.ConsulData,
		EnableLocalScriptChecks: true,
		Encrypt:                 string(extractedFiles[setup.GossipKey]),
		KeyFile:                 s.ConsulHome + "/" + setup.ConsulAgentCertificateKey,
		LogLevel:                defaultLogLevel,
		NodeName:                setup.ConsulNodeName(setup.Agent, zimbraHostname),
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

	if err := setup.SaveBindAddressConfiguration(s.MutableConfigFile, s.BindAddress); err != nil {
		return nil, err
	}

	if err := systemd.StartSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit); err != nil {
		return nil, errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
	}
	aclBootstrapToken := setup.ACLTokenCreation{}
	if err := json.Unmarshal(extractedFiles[setup.ConsulAclBootstrap], &aclBootstrapToken); err != nil {
		return nil, errors.WithMessagef(err, "unable to decode ACL Bootstrap token")
	}

	token, err := setup.CreateACLToken(d.CreateCommand, setup.Agent, zimbraHostname, aclBootstrapToken.SecretID)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to create ACL policy for this agent")
	}
	err = setup.SetACLToken(d.CreateCommand, token, aclBootstrapToken.SecretID)
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
	// FIXME the ownership of the file should be fixed! + 0600 perm should be used
	if err := ioutil.WriteFile(destination, consulAgentBs, os.FileMode(0644)); err != nil {
		return errors.WithMessagef(err, "unable to save generated configuration file in %s", destination)
	}
	return err
}
