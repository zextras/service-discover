package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/command/setup"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/systemd"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bitbucket.org/zextras/service-discover/cli/server/config"
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"github.com/pkg/errors"
	"net"
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
	firstInstance bool //TODO should be unused&removed in future
	Password      string
	BindAddress   string
}

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

// Setup command allows the final user to perform first time or add a server to an already existing cluster
type Setup struct {
	ConsulConfigDir   string `kong:"-"`
	ConsulHome        string `kong:"-"`
	LocalConfigPath   string `kong:"-"`
	ConsulData        string `kong:"-"`
	ConsulFileConfig  string `kong:"-"`
	ClusterCredential string `kong:"-"`
	MutableConfigFile string `kong:"-"`

	Wizard        bool `help:"Initialize the setup in interactive mode. All the non interactive flags will be ignored if this is set"`
	FirstInstance bool `help:"Configure the note as first of the cluster initialization"`

	Password    string `help:"Set a custom password for the encrypted secret files. If none is set, a random one will be generated and printed"`
	BindAddress string `arg optional help:"The binding address to bind service-discoverd daemon"`
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

func gatherInputs(d interactiveDependencies) (*setupConfiguration, error) {
	scanner := bufio.NewScanner(d.Reader())
	fmt.Fprintf(d.Writer(), "This is the setup for a new instance discover server.\n")
	fmt.Fprintf(d.Writer(), "Is this the first instance? [Y] ")
	input := strings.ToUpper(readUserInput(scanner))
	firstInstance := false
	if input == "" || input == "Y" {
		firstInstance = true
	}

	bindAddress, err := wizardBindAddressSelection(d, scanner)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(d.Writer(), "Create the cluster credentials password (will be used for setups): ")
	password := readUserInput(scanner) // FIXME avoid echoing password in tty

	return &setupConfiguration{
		firstInstance: firstInstance,
		Password:      password,
		BindAddress:   bindAddress,
	}, nil
}

// Run method runs the Setup command with the flags and settings passed by Kong.
func (s *Setup) Run(commonFlags *command.GlobalCommonFlags) error {
	d := realDependencies{}

	err := s.preRun(d)
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

		s.FirstInstance = inputs.firstInstance
		s.Password = inputs.Password
		s.BindAddress = inputs.BindAddress
	} else {
		if s.Password == "" && s.BindAddress == "" {
			return errors.New("missing arguments")
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

	if !s.Wizard {
		render, err := formatter.Render(out, commonFlags.Format)
		if err != nil {
			return err
		}
		fmt.Fprint(d.Writer(), render)
	}
	return nil
}

func (s *Setup) preRun(d businessDependencies) error {
	// We need to check that the executable is in $PATH
	cmd := d.CreateCommand(consulBin, "version")
	err := cmd.Run()
	if err != nil {
		return errors.New(fmt.Sprintf("unable to execute consul binary: %s", err))
	}

	if d.GetuidSyscall() != rootUid {
		return errors.New("this command must be executed as root")
	}

	//TODO: check if already present in LDAP or second setup

	return nil
}

func readUserInput(scanner *bufio.Scanner) string {
	scanner.Scan()
	return scanner.Text()
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

func wizardBindAddressSelection(d interactiveDependencies, scanner *bufio.Scanner) (string, error) {
	networks, err := setup.NonLoopbackInterfaces(d)
	if err != nil {
		return "", err
	}

	if len(networks) > 1 {
		fmt.Fprintf(d.Writer(), "Multiple network cards detected:\n")
	}

	for _, n := range networks {
		addrs, err := d.AddrResolver(n)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(d.Writer(), "%s %s\n", n.Name, addrsToSingleString(&addrs, ", "))
	}

	fmt.Fprintf(d.Writer(), "Specify the binding address for service discovery: ")
	bindingAddress := readUserInput(scanner)
	err = setup.CheckValidBindingAddress(d, networks, bindingAddress)
	if err != nil {
		return "", err
	}
	return bindingAddress, nil
}
