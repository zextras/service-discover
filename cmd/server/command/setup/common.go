// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	sharedsetup "github.com/zextras/service-discover/pkg/command/setup"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/term"
)

const (
	rootUID               = 0
	consulBin             = "/usr/bin/consul"
	certificateExpiration = 365 * 30
	defaultLogLevel       = "INFO"
)

type setupConfiguration struct {
	FirstInstance bool
	Password      string
	BindAddress   string
}

// Setup command allows the final user to perform first time or add a server to an already existing cluster.
type Setup struct {
	ConsulConfigDir   string `kong:"-"`
	ConsulHome        string `kong:"-"`
	LocalConfigPath   string `kong:"-"`
	ConsulData        string `kong:"-"`
	ConsulFileConfig  string `kong:"-"`
	ClusterCredential string `kong:"-"`
	MutableConfigFile string `kong:"-"`

	Password      string `help:"Custom password for encrypted secret files. If unset, one is generated"`
	BindAddress   string `arg:"" optional:"" help:"The binding address to bind service-discoverd daemon"`
	FirstInstance bool   `optional:"" default:"false" help:"Force the setup to behave as first server setup"`
}

// NewSetup creates a new Setup with default configuration values.
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

// Server-specific config types.
type autoEncrypt struct {
	AllowTLS bool `json:"allow_tls"`
}

type connectConfig struct {
	Enabled    bool   `json:"enabled"`
	CaProvider string `json:"ca_provider"`
}

// setupConfig is the server-specific configuration structure.
type setupConfig struct {
	ACLConfig               sharedsetup.ACLConfig   `json:"acl"`
	AutoEncrypt             autoEncrypt             `json:"auto_encrypt"`
	DataDir                 string                  `json:"data_dir"`
	EnableLocalScriptChecks bool                    `json:"enable_local_script_checks"`
	Encrypt                 string                  `json:"encrypt"`
	LogLevel                string                  `json:"log_level"`
	NodeName                string                  `json:"node_name"`
	Server                  bool                    `json:"server"`
	UIConfig                sharedsetup.UIConfig    `json:"ui_config"`
	Ports                   sharedsetup.PortsConfig `json:"ports"`
	Connect                 connectConfig           `json:"connect"`
	TLS                     sharedsetup.TLSConfig   `json:"tls"`
}

// nonInteractiveOutput is only an internal struct to output the result to the final user in an appropriate way.
type nonInteractiveOutput struct {
	EncFilepath string `json:"cluster_credentials"`
	Password    string `json:"credentials_password,omitempty"`
}

func (n *nonInteractiveOutput) PlainRender() (string, error) {
	return n.Password, nil
}

func (n *nonInteractiveOutput) JSONRender() (string, error) {
	return formatter.DefaultJSONRender(n)
}

func gatherInputs(deps sharedsetup.InteractiveDependencies, firstInstance bool) (*setupConfiguration, error) {
	bindAddress, err := wizardBindAddressSelection(deps)
	if err != nil {
		return nil, err
	}

	var password string

	if firstInstance {
		firstPassword :=
			term.MustRead(deps.Term().ReadPassword("Create the cluster credentials password (will be used for setups): "))
		password = term.MustRead(deps.Term().ReadPassword("Type the credential password again: "))

		if password != firstPassword {
			return nil, errors.New("passwords do not match")
		}
	} else {
		password = term.MustRead(deps.Term().ReadPassword("Insert the cluster credential password: "))
	}

	return &setupConfiguration{
		Password:    password,
		BindAddress: bindAddress,
	}, nil
}

// Run method runs the Setup command with the flags and settings passed by Kong.
func (s *Setup) Run(commonFlags *command.GlobalCommonFlags) error {
	userInterface, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return err
	}

	defer userInterface.Close()

	dependency := sharedsetup.RealDependencies{
		UI: &userInterface,
	}

	err = preRun(dependency)
	if err != nil {
		return err
	}

	if s.Password == "" && s.BindAddress == "" {
		return errors.New("missing arguments")
	}

	// if manually specified do not check it
	if !s.FirstInstance {
		s.FirstInstance, err = s.isFirstInstance(dependency)
		if err != nil {
			return err
		}
	}

	var out formatter.Formatter
	if s.FirstInstance {
		out, err = s.firstSetup(dependency)
	} else {
		out, err = s.importSetup(dependency)
	}

	if err != nil {
		return err
	}

	render, err := formatter.Render(out, commonFlags.Format)
	if err != nil {
		return err
	}

	fmt.Fprint(dependency.Writer(), render)

	return nil
}

func (s *Setup) isFirstInstance(deps sharedsetup.BusinessDependencies) (bool, error) {
	_, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		zimbraLocalConfig, err := carbonio.LoadLocalConfig(s.LocalConfigPath)
		if err != nil {
			return false, err
		}

		ldapHandler := deps.LdapHandler(zimbraLocalConfig)

		servers, err := ldapHandler.QueryAllServersWithService(carbonio.ServiceDiscoverServiceName)
		if err != nil {
			return false, err
		}

		return len(servers) == 0, nil
	}

	return false, nil
}

func preRun(deps sharedsetup.BusinessDependencies) error {
	// We need to check that the executable is in $PATH
	cmd := deps.CreateCommand(consulBin, "version")

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

func addrsToSingleString(addrs *[]net.Addr, sep string) string {
	strAddrs := make([]string, len(*addrs))

	for i, a := range *addrs {
		if a.String() != "" {
			strAddrs[i] = a.String()
		}
	}

	return strings.Join(strAddrs, sep)
}

// generateGossipKey is directly taken from the way Consul generates it.
func generateGossipKey() (string, error) {
	key := make([]byte, 32)

	num, err := rand.Reader.Read(key)
	if err != nil {
		return "", errors.Errorf("error reading random data: %s", err)
	}

	if num != 32 {
		return "", errors.New("couldn't read enough entropy. Generate more entropy")
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

func wizardBindAddressSelection(deps sharedsetup.InteractiveDependencies) (string, error) {
	networks, err := command.NonLoopbackInterfaces(deps)
	if err != nil {
		return "", err
	}

	if len(networks) > 1 {
		term.MustWrite(fmt.Fprintf(deps.Term(), "Multiple network cards detected:\n"))
	}

	for _, net := range networks {
		addrs, err := deps.AddrResolver(net)
		if err != nil {
			return "", err
		}

		term.MustWrite(fmt.Fprintf(deps.Term(), "%s %s\n", net.Name, addrsToSingleString(&addrs, ", ")))
	}

	term.MustWrite(fmt.Fprintf(deps.Term(), "Specify the binding address for service discovery: "))
	bindingAddress := term.MustRead(deps.Term().ReadLine())

	err = command.CheckValidBindingAddress(deps, networks, bindingAddress)
	if err != nil {
		return "", err
	}

	return bindingAddress, nil
}

// generateCertificateAndConfig creates the TLS certificates for consul and
// finally it generates the gossip key. This ensure secure communications
// inside Consul.
func (s *Setup) generateCertificateAndConfig(deps sharedsetup.BusinessDependencies,
	zimbraHostname string, gossipKey string) (*setupConfig, error) {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)

	err := exec.InPath(
		deps.CreateCommand(consulBin,
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

	err = permissions.SetStrictPermissions(deps, filepath.Join(s.ConsulHome, command.ConsulServerCertificateKey))
	if err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(deps, filepath.Join(s.ConsulHome, command.ConsulServerCertificate))
	if err != nil {
		return nil, err
	}

	consulConfigFile := &setupConfig{
		ACLConfig:               sharedsetup.DefaultACLConfig(),
		AutoEncrypt:             autoEncrypt{AllowTLS: true},
		DataDir:                 s.ConsulData,
		EnableLocalScriptChecks: true,
		Encrypt:                 gossipKey,
		LogLevel:                defaultLogLevel,
		NodeName:                command.ConsulNodeName(command.Server, zimbraHostname),
		Server:                  true,
		UIConfig:                sharedsetup.DefaultUIConfig(),
		Ports:                   sharedsetup.DefaultPortsConfig(),
		Connect:                 connectConfig{Enabled: true},
		TLS: sharedsetup.TLSConfig{
			Defaults: sharedsetup.TLSDefaults{
				CaFile:         s.ConsulHome + "/" + command.ConsulCA,
				CertFile:       s.ConsulHome + "/" + command.ConsulServerCertificate,
				KeyFile:        s.ConsulHome + "/" + command.ConsulServerCertificateKey,
				VerifyIncoming: true,
				VerifyOutgoing: true,
			},
			InternalRPC: sharedsetup.DefaultTLSInternalRPC(),
		},
	}

	return consulConfigFile, nil
}

// writePasswordFile writes the password to a file with restricted permissions (0400).
// This is used during setup to store the cluster credential password for later use.
func writePasswordFile(consulHome, password string) error {
	return os.WriteFile(consulHome+"/password", []byte(password), 0400)
}
