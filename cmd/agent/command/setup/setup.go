// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/cmd/agent/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	sharedsetup "github.com/zextras/service-discover/pkg/command/setup"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/term"
)

const (
	rootUID               = 0
	certificateExpiration = 365 * 30
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

type setupConfiguration struct {
	Password    string
	BindAddress string
}

// setupConfig is the agent-specific configuration structure.
type setupConfig struct {
	ACLConfig               sharedsetup.ACLConfig   `json:"acl"`
	DataDir                 string                  `json:"data_dir"`
	EnableLocalScriptChecks bool                    `json:"enable_local_script_checks"`
	Encrypt                 string                  `json:"encrypt"`
	LogLevel                string                  `json:"log_level"`
	NodeName                string                  `json:"node_name"`
	Server                  bool                    `json:"server"`
	UIConfig                sharedsetup.UIConfig    `json:"ui_config"`
	Ports                   sharedsetup.PortsConfig `json:"ports"`
	TLS                     sharedsetup.TLSConfig   `json:"tls"`
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

func gatherInputs(deps sharedsetup.InteractiveDependencies) (*setupConfiguration, error) {
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

func preRun(_ string, deps sharedsetup.BusinessDependencies) error {
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

func (s *Setup) Run(commonFlags *command.GlobalCommonFlags) error {
	userInterface, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return err
	}

	defer userInterface.Close()

	deps := sharedsetup.RealDependencies{
		UI: &userInterface,
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

func (s *Setup) createTLSCertificate(deps sharedsetup.BusinessDependencies, caFile, caKeyFile *os.File) error {
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
func (s *Setup) setup(deps sharedsetup.BusinessDependencies) (formatter.Formatter, error) {
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

func (s *Setup) setupNetworkAndConfig(deps sharedsetup.BusinessDependencies) (string, carbonio.LdapHandler, error) {
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

func (s *Setup) setupCertificates(deps sharedsetup.BusinessDependencies, extractedFiles map[string][]byte) error {
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
	deps sharedsetup.BusinessDependencies,
	extractedFiles map[string][]byte,
	zimbraHostname string,
) error {
	consulAgentConfig := &setupConfig{
		ACLConfig:               sharedsetup.DefaultACLConfig(),
		DataDir:                 s.ConsulData,
		EnableLocalScriptChecks: true,
		Encrypt:                 string(extractedFiles[command.GossipKey]),
		LogLevel:                defaultLogLevel,
		NodeName:                command.ConsulNodeName(command.Agent, zimbraHostname),
		Server:                  false,
		UIConfig:                sharedsetup.DefaultUIConfig(),
		Ports:                   sharedsetup.DefaultPortsConfig(),
		TLS: sharedsetup.TLSConfig{
			Defaults: sharedsetup.TLSDefaults{
				CaFile:         s.ConsulHome + "/" + command.ConsulCA,
				CertFile:       s.ConsulHome + "/" + command.ConsulAgentCertificate,
				KeyFile:        s.ConsulHome + "/" + command.ConsulAgentCertificateKey,
				VerifyIncoming: true,
				VerifyOutgoing: true,
			},
			InternalRPC: sharedsetup.DefaultTLSInternalRPC(),
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

	return sharedsetup.SaveBindAddressWithPermissions(deps, s.MutableConfigFile, s.BindAddress)
}

func (s *Setup) startServiceDiscover(deps sharedsetup.BusinessDependencies) (bool, error) {
	return sharedsetup.StartServiceDiscoverMode(deps, "agent")
}

func (s *Setup) setupACLTokens(
	deps sharedsetup.BusinessDependencies,
	extractedFiles map[string][]byte,
	zimbraHostname string,
	isContainer bool,
) error {
	err := sharedsetup.ACLTokenFromExtractedFiles(deps, command.Agent, zimbraHostname, extractedFiles)
	if err != nil {
		return err
	}

	return sharedsetup.EnableSystemdUnitIfNotContainer(deps, isContainer)
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
