// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/spire"
	"github.com/zextras/service-discover/pkg/systemd"
)

const (
	stdinMarker = "-"
	// maxBundleSize bounds what is read from the bundle file or from the standard input.
	maxBundleSize   = 1 << 20
	spireFileMode   = 0640
	spireSecretMode = 0600
	// introTokenFile is the file Consul reads the JWT-SVID to authenticate its auto_config request from.
	introTokenFile = "intro-token"
)

// spirePollInterval and spireStartTimeout drive the wait for the local SPIRE agent to attest itself and serve the
// Workload API.
var (
	spirePollInterval = 500 * time.Millisecond
	spireStartTimeout = 60 * time.Second
)

type autoConfigClient struct {
	Enabled         bool     `json:"enabled"`
	IntroTokenFile  string   `json:"intro_token_file"`
	ServerAddresses []string `json:"server_addresses"`
}

type joinTLSDefaults struct {
	CaFile         string `json:"ca_file"`
	VerifyOutgoing bool   `json:"verify_outgoing"`
}

type joinTLS struct {
	Defaults    joinTLSDefaults `json:"defaults"`
	InternalRPC tlsInternalRPC  `json:"internal_rpc"`
}

// joinConfig is the Consul client configuration of an agent joined through SPIRE: it carries neither the gossip
// key nor a certificate, since everything else is delivered by the servers through auto_config.
type joinConfig struct {
	AutoConfig              autoConfigClient `json:"auto_config"`
	DataDir                 string           `json:"data_dir"`
	EnableLocalScriptChecks bool             `json:"enable_local_script_checks"`
	LogLevel                string           `json:"log_level"`
	NodeName                string           `json:"node_name"`
	Server                  bool             `json:"server"`
	UIConfig                uiConfig         `json:"ui_config"`
	Ports                   portsConfig      `json:"ports"`
	TLS                     joinTLS          `json:"tls"`
}

func (s *Setup) runJoin(commonFlags *command.GlobalCommonFlags) error {
	return s.runJoinWith(commonFlags, &realDependencies{}, os.Stdout)
}

// runJoinWith checks the flags and the system, joins the cluster and renders the result on writer.
func (s *Setup) runJoinWith(commonFlags *command.GlobalCommonFlags, deps businessDependencies, writer io.Writer) error {
	if s.Wizard || s.Password != "" {
		return errors.New("--join cannot be combined with --wizard nor --password")
	}

	if s.BindAddress == "" {
		return errors.New("missing arguments: the binding address is required")
	}

	err := preRun(s.ClusterCredential, deps)
	if err != nil {
		return err
	}

	out, err := s.join(deps)
	if err != nil {
		return err
	}

	render, err := formatter.Render(out, commonFlags.Format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(writer, render)

	return err
}

// readJoinBundle reads and validates the join bundle from the given file, or from Stdin if the source is "-".
func (s *Setup) readJoinBundle() (*spire.JoinBundle, error) {
	var (
		content []byte
		err     error
	)

	if s.Join == stdinMarker {
		content, err = io.ReadAll(io.LimitReader(s.Stdin, maxBundleSize))
	} else {
		content, err = readLimitedFile(s.Join)
	}

	if err != nil {
		return nil, errors.WithMessage(err, "unable to read the join bundle")
	}

	bundle, err := spire.DecodeJoinBundle(string(content))
	if err != nil {
		return nil, errors.WithMessage(err, "invalid join bundle")
	}

	return bundle, nil
}

func readLimitedFile(path string) ([]byte, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	defer file.Close()

	return io.ReadAll(io.LimitReader(file, maxBundleSize))
}

// join enrolls this host with a SPIRE join bundle: the local SPIRE agent attests itself with the one-time token,
// the JWT-SVID it issues for this Consul node is used by Consul to join the cluster through auto_config. No cluster
// credentials, LDAP access nor ACL token are involved.
func (s *Setup) join(deps businessDependencies) (formatter.Formatter, error) {
	networks, err := command.NonLoopbackInterfaces(deps)
	if err != nil {
		return nil, err
	}

	err = command.CheckValidBindingAddress(deps, networks, s.BindAddress)
	if err != nil {
		return nil, err
	}

	bundle, err := s.readJoinBundle()
	if err != nil {
		return nil, err
	}

	if command.CheckDockerContainer() && !testingMode {
		return nil, errors.New("joining through SPIRE is not supported inside a container")
	}

	spireHost, spirePort, err := splitServerAddress(bundle.SpireServer)
	if err != nil {
		return nil, err
	}

	caPath := filepath.Join(s.ConsulHome, command.ConsulCA)

	err = s.writeStrict(deps, caPath, []byte(bundle.ConsulCA))
	if err != nil {
		return nil, err
	}

	err = command.SaveBindAddressConfiguration(s.MutableConfigFile, s.BindAddress)
	if err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(deps, s.MutableConfigFile)
	if err != nil {
		return nil, err
	}

	token, err := s.attestAndFetchToken(deps, bundle, spireHost, spirePort)
	if err != nil {
		return nil, err
	}

	err = s.writeStrict(deps, filepath.Join(s.ConsulHome, introTokenFile), []byte(token))
	if err != nil {
		return nil, err
	}

	err = s.writeJoinConfig(deps, bundle)
	if err != nil {
		return nil, err
	}

	err = systemd.StartSystemdUnit(deps.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return nil, errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
	}

	err = systemd.EnableSystemdUnit(deps.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return nil, errors.Errorf("unable to enable %s unit: %s", serviceDiscoverUnit, err)
	}

	return &formatter.EmptyFormatter{}, nil
}

// attestAndFetchToken configures and starts the local SPIRE agent, and returns the JWT-SVID it issues to the Consul
// agent of this node. The join token is only stored in the SPIRE agent configuration, never on a command line.
func (s *Setup) attestAndFetchToken(
	deps businessDependencies, bundle *spire.JoinBundle, spireHost string, spirePort int,
) (string, error) {
	_, err := spire.WriteFile(deps, s.SpireConfigDir, spire.BundleFile,
		[]byte(bundle.SpireBundle), spire.AgentUser, spireFileMode)
	if err != nil {
		return "", err
	}

	_, err = spire.WriteFile(deps, s.SpireConfigDir, spire.AgentConfFile,
		[]byte(spire.AgentConf(s.SpireConfigDir, spireHost, spirePort, bundle.JoinToken)),
		spire.AgentUser, spireSecretMode)
	if err != nil {
		return "", err
	}

	err = systemd.StartSystemdUnit(deps.SystemdUnitHandler, spire.AgentUnit)
	if err != nil {
		return "", errors.WithMessagef(err, "unable to start %s", spire.AgentUnit)
	}

	err = systemd.EnableSystemdUnit(deps.SystemdUnitHandler, spire.AgentUnit)
	if err != nil {
		return "", errors.Errorf("unable to enable %s unit: %s", spire.AgentUnit, err)
	}

	token, err := spire.NewAgentCLI(deps.CreateCommand).WaitJWTSVID(
		spire.AudienceAutoConfig, spire.ConsulID(bundle.NodeName), spireStartTimeout, spirePollInterval)
	if err != nil {
		return "", errors.WithMessage(err, "the join token is valid only once: if it has been consumed generate "+
			"a new bundle with 'service-discover join-token' before retrying")
	}

	return token, nil
}

func (s *Setup) writeJoinConfig(deps businessDependencies, bundle *spire.JoinBundle) error {
	consulAgentConfig := &joinConfig{
		AutoConfig: autoConfigClient{
			Enabled:         true,
			IntroTokenFile:  filepath.Join(s.ConsulHome, introTokenFile),
			ServerAddresses: bundle.ConsulServers,
		},
		DataDir:                 s.ConsulData,
		EnableLocalScriptChecks: true,
		LogLevel:                defaultLogLevel,
		NodeName:                bundle.NodeName,
		Server:                  false,
		UIConfig:                uiConfig{Enabled: true},
		Ports:                   portsConfig{Grpc: 8502, GrpcTLS: 8503},
		TLS: joinTLS{
			Defaults: joinTLSDefaults{
				CaFile:         filepath.Join(s.ConsulHome, command.ConsulCA),
				VerifyOutgoing: true,
			},
			InternalRPC: tlsInternalRPC{VerifyServerHostname: true},
		},
	}

	err := writeSetupConfig(consulAgentConfig, s.ConsulFileConfig)
	if err != nil {
		return err
	}

	return permissions.SetStrictPermissions(deps, s.ConsulFileConfig)
}

// writeStrict writes a file only readable by the service-discover user.
func (s *Setup) writeStrict(deps businessDependencies, path string, content []byte) error {
	err := os.WriteFile(path, content, os.FileMode(spireSecretMode))
	if err != nil {
		return errors.WithMessagef(err, "unable to write %s", path)
	}

	return permissions.SetStrictPermissions(deps, path)
}

func splitServerAddress(address string) (string, int, error) {
	host, portString, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, errors.WithMessagef(err, "invalid SPIRE server address %q in the join bundle", address)
	}

	port, err := strconv.Atoi(portString)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, errors.Errorf("invalid SPIRE server port in the join bundle address %q", address)
	}

	return host, port, nil
}
