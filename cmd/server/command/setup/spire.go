// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/spire"
	"github.com/zextras/service-discover/pkg/systemd"
)

const (
	spireJoinTokenTTL = 10 * time.Minute
	spireFileMode     = 0640
	spireSecretMode   = 0600
)

// spirePollInterval and spireStartTimeout drive the waits for the SPIRE components to become ready.
var (
	spirePollInterval = 500 * time.Millisecond
	spireStartTimeout = 60 * time.Second
	waitOIDCDiscovery = spire.WaitOIDCDiscovery
)

// bindIP extracts the IP from a binding address, that may be given in CIDR notation.
func bindIP(bindAddress string) (net.IP, error) {
	ip := net.ParseIP(strings.Split(bindAddress, "/")[0])
	if ip == nil {
		return nil, errors.Errorf("invalid binding address '%s'", bindAddress)
	}

	return ip, nil
}

// manageSpireUnits reports whether the SPIRE units have to be handled by systemd. Like Consul, SPIRE is not started
// by the setup when it runs inside a container.
func manageSpireUnits() bool {
	return !command.CheckDockerContainer() || testingMode
}

func startAndEnableUnit(deps businessDependencies, unit string) error {
	err := systemd.StartSystemdUnit(deps.SystemdUnitHandler, unit)
	if err != nil {
		return errors.WithMessagef(err, "unable to start %s", unit)
	}

	err = systemd.EnableSystemdUnit(deps.SystemdUnitHandler, unit)
	if err != nil {
		return errors.Errorf("unable to enable %s unit: %s", unit, err)
	}

	return nil
}

// setupSpireServer configures and starts the SPIRE server and its OIDC discovery provider on the first server. The
// OIDC endpoint is served with a certificate signed by the Consul CA, that must already exist. It returns the OIDC
// discovery URL Consul has to trust, once it is reachable.
func (s *Setup) setupSpireServer(deps businessDependencies, hostname, bindAddress string) (string, error) {
	listenIP, err := bindIP(bindAddress)
	if err != nil {
		return "", err
	}

	caPEM, err := os.ReadFile(filepath.Join(s.ConsulHome, command.ConsulCA)) // #nosec
	if err != nil {
		return "", errors.WithMessage(err, "unable to read the Consul CA")
	}

	caKeyPEM, err := os.ReadFile(filepath.Join(s.ConsulHome, command.ConsulCAKey)) // #nosec
	if err != nil {
		return "", errors.WithMessage(err, "unable to read the Consul CA key")
	}

	certPEM, keyPEM, err := spire.SignServingCert(
		caPEM, caKeyPEM, hostname, listenIP, time.Duration(certificateExpiration)*24*time.Hour,
	)
	if err != nil {
		return "", err
	}

	files := []struct {
		name    string
		content []byte
		mode    os.FileMode
	}{
		{spire.OIDCCertFile, certPEM, spireFileMode},
		{spire.OIDCKeyFile, keyPEM, spireSecretMode},
		{spire.ServerConfFile, []byte(spire.ServerConf(listenIP.String(), hostname)), spireFileMode},
		{spire.OIDCConfFile, []byte(spire.OIDCConf(s.SpireConfigDir, listenIP.String(), hostname)), spireFileMode},
	}

	for _, file := range files {
		_, err = spire.WriteFile(deps, s.SpireConfigDir, file.name, file.content, spire.ServerUser, file.mode)
		if err != nil {
			return "", err
		}
	}

	oidcURL := spire.OIDCURL(hostname)

	if !manageSpireUnits() {
		return oidcURL, nil
	}

	err = startAndEnableUnit(deps, spire.ServerUnit)
	if err != nil {
		return "", err
	}

	err = spire.NewServerCLI(deps.CreateCommand).WaitHealthy(spireStartTimeout, spirePollInterval)
	if err != nil {
		return "", err
	}

	err = startAndEnableUnit(deps, spire.OIDCUnit)
	if err != nil {
		return "", err
	}

	err = waitOIDCDiscovery(oidcURL, caPEM, spireStartTimeout, spirePollInterval)
	if err != nil {
		return "", err
	}

	return oidcURL, nil
}

// setupSpireAgent attests the SPIRE agent of this server to the local SPIRE server with a one-time join token and
// starts it, so that the services hosted on the server can get their identity. It needs the SPIRE server running.
func (s *Setup) setupSpireAgent(deps businessDependencies, hostname, bindAddress string) error {
	listenIP, err := bindIP(bindAddress)
	if err != nil {
		return err
	}

	cli := spire.NewServerCLI(deps.CreateCommand)

	err = cli.WaitHealthy(spireStartTimeout, spirePollInterval)
	if err != nil {
		return err
	}

	bundle, err := cli.BundlePEM()
	if err != nil {
		return err
	}

	token, err := cli.GenerateJoinToken(spire.NodeID(hostname), spireJoinTokenTTL)
	if err != nil {
		return err
	}

	_, err = spire.WriteFile(deps, s.SpireConfigDir, spire.BundleFile, []byte(bundle), spire.AgentUser, spireFileMode)
	if err != nil {
		return err
	}

	_, err = spire.WriteFile(deps, s.SpireConfigDir, spire.AgentConfFile,
		[]byte(spire.AgentConf(s.SpireConfigDir, listenIP.String(), token)), spire.AgentUser, spireSecretMode)
	if err != nil {
		return err
	}

	return startAndEnableUnit(deps, spire.AgentUnit)
}
