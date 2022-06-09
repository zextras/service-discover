package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/exec"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/permissions"
	"bitbucket.org/zextras/service-discover/cli/lib/systemd"
	"bitbucket.org/zextras/service-discover/cli/lib/term"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bitbucket.org/zextras/service-discover/cli/server/config"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"github.com/pkg/errors"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	rootUid               = 0
	consulBin             = "/usr/bin/consul"
	certificateExpiration = 365 * 30
	defaultLogLevel       = "INFO"
	serviceDiscoverUnit   = "service-discover.service"
)

type setupConfiguration struct {
	FirstInstance bool
	Password      string
	BindAddress   string
}

func NewSetup() Setup {
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

// Setup command allows the final user to perform first time or add a server to an already existing cluster
type Setup struct {
	ConsulConfigDir   string `kong:"-"`
	ConsulHome        string `kong:"-"`
	LocalConfigPath   string `kong:"-"`
	ConsulData        string `kong:"-"`
	ConsulFileConfig  string `kong:"-"`
	ClusterCredential string `kong:"-"`
	MutableConfigFile string `kong:"-"`

	Password      string `help:"Set a custom password for the encrypted secret files. If none is set, a random one will be generated and printed"`
	BindAddress   string `arg optional help:"The binding address to bind service-discoverd daemon"`
	FirstInstance bool   `optional default:"false" help:"Force the setup to behave as this was the first server setup"`
}

type autoEncrypt struct {
	AllowTLS bool `json:"allow_tls"`
}

type aclConfig struct {
	Enabled                bool   `json:"enabled"`
	DefaultPolicy          string `json:"default_policy"`
	DownPolicy             string `json:"down_policy"`
	EnableTokenPersistence bool   `json:"enable_token_persistence"`
}

type uiConfig struct {
	Enabled bool `json:"enabled"`
}

type portsConfig struct {
	Grpc int `json:"grpc"`
}

type connectConfig struct {
	Enabled    bool   `json:"enabled"`
	CaProvider string `json:"ca_provider"`
}

type setupConfig struct {
	AclConfig               aclConfig     `json:"acl"`
	AutoEncrypt             autoEncrypt   `json:"auto_encrypt,omitempty"`
	CaFile                  string        `json:"ca_file"`
	CertFile                string        `json:"cert_file"`
	DataDir                 string        `json:"data_dir"`
	EnableLocalScriptChecks bool          `json:"enable_local_script_checks"`
	Encrypt                 string        `json:"encrypt"`
	KeyFile                 string        `json:"key_file"`
	LogLevel                string        `json:"log_level"`
	NodeName                string        `json:"node_name"`
	Server                  bool          `json:"server"`
	VerifyIncoming          bool          `json:"verify_incoming"`
	VerifyOutgoing          bool          `json:"verify_outgoing"`
	VerifyServerHostname    bool          `json:"verify_server_hostname"`
	UiConfig                uiConfig      `json:"ui_config"`
	Ports                   portsConfig   `json:"ports"`
	Connect                 connectConfig `json:"connect"`
}

// nonInteractiveOutput is only an internal struct to output the result to the final user in an appropriate way
type nonInteractiveOutput struct {
	EncFilepath string `json:"cluster_credentials"`
	Password    string `json:"credentials_password,omitempty"`
}

func (n *nonInteractiveOutput) PlainRender() (string, error) {
	return n.Password, nil
}

func (n *nonInteractiveOutput) JsonRender() (string, error) {
	return formatter.DefaultJsonRender(n)
}

func gatherInputs(d interactiveDependencies, firstInstance bool) (*setupConfiguration, error) {
	bindAddress, err := wizardBindAddressSelection(d)
	if err != nil {
		return nil, err
	}

	var password string
	if firstInstance {
		firstPassword := term.MustRead(d.Term().ReadPassword("Create the cluster credentials password (will be used for setups): "))
		password = term.MustRead(d.Term().ReadPassword("Type the credential password again: "))
		if password != firstPassword {
			return nil, errors.New("passwords do not match")
		}
	} else {
		password = term.MustRead(d.Term().ReadPassword("Insert the cluster credential password: "))
	}

	return &setupConfiguration{
		Password:    password,
		BindAddress: bindAddress,
	}, nil
}

// Run method runs the Setup command with the flags and settings passed by Kong.
func (s *Setup) Run(commonFlags *command.GlobalCommonFlags) error {
	ui, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return err
	}
	defer ui.Close()
	d := realDependencies{
		ui: &ui,
	}

	err = preRun(d)
	if err != nil {
		return err
	}

	if s.Password == "" && s.BindAddress == "" {
		return errors.New("missing arguments")
	}

	//if manually specified do not check it
	if !s.FirstInstance {
		s.FirstInstance, err = s.isFirstInstance(d)
		if err != nil {
			return err
		}
	}

	var out formatter.Formatter
	if s.FirstInstance {
		out, err = s.firstSetup(d)
	} else {
		out, err = s.importSetup(d)
	}
	if err != nil {
		return err
	}

	render, err := formatter.Render(out, commonFlags.Format)
	if err != nil {
		return err
	}
	fmt.Fprint(d.Writer(), render)
	return nil
}

func (s *Setup) isFirstInstance(d businessDependencies) (bool, error) {
	_, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		zimbraLocalConfig, err := zimbra.LoadLocalConfig(s.LocalConfigPath)
		if err != nil {
			return false, errors.New(fmt.Sprintf("unable to read Zimbra local config: %s", err))
		}
		ldapHandler := d.LdapHandler(zimbraLocalConfig)
		servers, err := ldapHandler.QueryAllServersWithService(zimbra.ServiceDiscoverServiceName)
		if err != nil {
			return false, err
		}
		return len(servers) == 0, nil
	} else {
		return false, nil
	}
}

func preRun(d businessDependencies) error {
	// We need to check that the executable is in $PATH
	cmd := d.CreateCommand(consulBin, "version")
	err := cmd.Run()
	if err != nil {
		return errors.New(fmt.Sprintf("unable to execute consul binary: %s", err))
	}

	if d.GetuidSyscall() != rootUid {
		return errors.New("this command must be executed as root")
	}

	_, err = os.Stat(config.ConsultFileConfig)
	if err == nil {
		return errors.New(fmt.Sprintf("setup of service-discover already perfomed, manually reset and try again."))
	}

	return nil
}

func addrsToSingleString(addrs *[]net.Addr, sep string) string {
	strAddrs := make([]string, len(*addrs))
	for i, a := range *addrs {
		if a.String() != "" {
			strAddrs[i] = a.String()
		}
	}
	return strings.Join(strAddrs, sep)
}

// generateGossipKey is directly taken from the way Consul generates it
func generateGossipKey() (string, error) {
	key := make([]byte, 32)
	n, err := rand.Reader.Read(key)
	if err != nil {
		return "", errors.New(fmt.Sprintf("error reading random data: %s", err))
	}
	if n != 32 {
		return "", errors.New(fmt.Sprintf("couldn't read enough entropy. Generate more entropy!"))
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

// retrieveZimbraHostname returns the zimbra.LocalConfigServerHostname value, but only after checking that the
// LDAP server is up
func (s *Setup) retrieveZimbraHostname(localConfig zimbra.LocalConfig, ldapHandler zimbra.LdapHandler) (string, error) {
	err := ldapHandler.CheckServerAvailability(true)
	if err != nil {
		return "", errors.New("unable to connect to ldap: " + err.Error())
	}
	return localConfig.Value(zimbra.LocalConfigServerHostname), nil
}

func (s *Setup) addServiceInLDAP(ldap zimbra.LdapHandler, zimbraHostname string) error {
	err := ldap.AddService(zimbraHostname, zimbra.ServiceDiscoverServiceName)
	if err != nil {
		return errors.New("cannot add service in ldap: " + err.Error())
	}
	return nil
}

func (s *Setup) enableServiceDiscoverd(d businessDependencies) error {
	err := systemd.EnableSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to enable %s unit: %s", serviceDiscoverUnit, err))
	}
	return nil
}

func wizardBindAddressSelection(d interactiveDependencies) (string, error) {
	networks, err := command.NonLoopbackInterfaces(d)
	if err != nil {
		return "", err
	}

	if len(networks) > 1 {
		term.MustWrite(fmt.Fprintf(d.Term(), "Multiple network cards detected:\n"))
	}

	for _, n := range networks {
		addrs, err := d.AddrResolver(n)
		if err != nil {
			return "", err
		}

		term.MustWrite(fmt.Fprintf(d.Term(), "%s %s\n", n.Name, addrsToSingleString(&addrs, ", ")))
	}

	term.MustWrite(fmt.Fprintf(d.Term(), "Specify the binding address for service discovery: "))
	bindingAddress := term.MustRead(d.Term().ReadLine())
	err = command.CheckValidBindingAddress(d, networks, bindingAddress)
	if err != nil {
		return "", err
	}
	return bindingAddress, nil
}

// generateCertificateAndConfig creates the TLS certificates for consul and finally it generates the gossip key. This ensure secure
// communications inside Consul
func (s *Setup) generateCertificateAndConfig(d businessDependencies, zimbraHostname string, gossipKey string) (*setupConfig, error) {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
	err := exec.InPath(
		d.CreateCommand(consulBin,
			"tls",
			"cert",
			"create",
			certificateDaysFlag,
			"-server"),
		s.ConsulHome,
	)
	if err != nil {
		return nil, errors.New("unable to create a valid certificate with Consul")
	}

	err = permissions.SetStrictPermissions(d, filepath.Join(s.ConsulHome, command.ConsulServerCertificateKey))
	if err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(d, filepath.Join(s.ConsulHome, command.ConsulServerCertificate))
	if err != nil {
		return nil, err
	}

	consulConfigFile := &setupConfig{
		AclConfig: aclConfig{
			Enabled:                true,
			EnableTokenPersistence: true,
			DefaultPolicy:          "deny",
			DownPolicy:             "extend-cache",
		},
		AutoEncrypt:             autoEncrypt{AllowTLS: true},
		CaFile:                  s.ConsulHome + "/" + command.ConsulCA,
		CertFile:                s.ConsulHome + "/" + command.ConsulServerCertificate,
		DataDir:                 s.ConsulData,
		EnableLocalScriptChecks: true,
		Encrypt:                 gossipKey,
		KeyFile:                 s.ConsulHome + "/" + command.ConsulServerCertificateKey,
		LogLevel:                defaultLogLevel,
		NodeName:                command.ConsulNodeName(command.Server, zimbraHostname),
		Server:                  true,
		VerifyIncoming:          true,
		VerifyOutgoing:          true,
		VerifyServerHostname:    true,
		UiConfig:                uiConfig{Enabled: true},
		Ports:                   portsConfig{Grpc: 8502},
		Connect:                 connectConfig{Enabled: true},
	}
	return consulConfigFile, nil
}
