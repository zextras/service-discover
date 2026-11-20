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
	"github.com/zextras/service-discover/pkg/mesh"
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
	waitJWKS          = spire.WaitJWKS
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

// registerMesh is replaced by the tests: it needs a Consul cluster.
var registerMesh = registerMeshServices

// registerMeshServices registers the services defined by service-discover in the cluster, with the bootstrap token:
// the ACL policies, the roles, the binding rules of the "spire" auth method, the service protocols and the
// intentions. Then it creates the SPIRE entries of the services of this server. The other nodes get theirs from the
// join token.
func registerMeshServices(deps businessDependencies, hostname, rootToken string) error {
	err := mesh.ApplyEmbedded(rootToken, spire.TrustDomain, nil, os.Stdout)
	if err != nil {
		return err
	}

	defs, err := mesh.Load()
	if err != nil {
		return err
	}

	_, err = mesh.EnsureEntries(spire.NewServerCLI(deps.CreateCommand), defs, spire.TrustDomain, hostname)

	return err
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

// highAvailability reports whether every Consul server runs its own SPIRE server, all of them sharing the same
// PostgreSQL datastore. Without a datastore only one server runs SPIRE, on a local SQLite database.
func (s *Setup) highAvailability() bool {
	return s.spireDSN != ""
}

// setupSpireServer configures and starts the SPIRE server and its OIDC discovery provider of this server. The OIDC
// endpoint is served with a certificate signed by the Consul CA, that must already exist. It returns the URL where
// Consul fetches the keys validating the JWT-SVIDs, once it is reachable.
//
// With a single SPIRE server that is its OIDC endpoint, open on the bind address for the other Consul servers. In
// high availability every Consul server validates against the provider of its own host: the provider listens on the
// loopback address only and the URL is the same on every server. The Consul CA is name constrained to the hostname of
// the first server (and, in clusters set up before SPIRE, to no hostname at all), so the certificate of the others is
// issued to "localhost" and to their IP addresses: without high availability they are reached through their IP.
// additional tells that the CA is not constrained to the hostname of this server.
//
//nolint:cyclop // sequential flow
func (s *Setup) setupSpireServer(
	deps businessDependencies, hostname, bindAddress string, additional bool,
) (string, error) {
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

	certHost, oidcListen, jwksURL := hostname, listenIP.String(), spire.JWKSURL(hostname)
	if s.highAvailability() {
		oidcListen, jwksURL = "127.0.0.1", spire.LocalJWKSURL
	}

	if additional {
		certHost = "localhost"

		if !s.highAvailability() {
			jwksURL = spire.JWKSURL(listenIP.String())
		}
	}

	certPEM, keyPEM, err := spire.SignServingCert(
		caPEM, caKeyPEM, certHost, listenIP, time.Duration(certificateExpiration)*24*time.Hour,
	)
	if err != nil {
		return "", err
	}

	serverConfMode := os.FileMode(spireFileMode)
	if s.highAvailability() {
		serverConfMode = spireSecretMode // it holds the credentials of the database
	}

	files := []struct {
		name    string
		content []byte
		mode    os.FileMode
	}{
		{spire.OIDCCertFile, certPEM, spireFileMode},
		{spire.OIDCKeyFile, keyPEM, spireSecretMode},
		{spire.ServerConfFile, []byte(spire.ServerConf(listenIP.String(), s.spireDSN)), serverConfMode},
		{spire.OIDCConfFile, []byte(spire.OIDCConf(s.SpireConfigDir, oidcListen, hostname)), spireFileMode},
	}

	for _, file := range files {
		_, err = spire.WriteFile(deps, s.SpireConfigDir, file.name, file.content, spire.ServerUser, file.mode)
		if err != nil {
			return "", err
		}
	}

	if !manageSpireUnits() {
		return jwksURL, nil
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

	err = waitJWKS(jwksURL, caPEM, spireStartTimeout, spirePollInterval)
	if err != nil {
		return "", err
	}

	return jwksURL, nil
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
		[]byte(spire.AgentConf(s.SpireConfigDir, listenIP.String(), spire.ServerPort, token)),
		spire.AgentUser, spireSecretMode)
	if err != nil {
		return err
	}

	return startAndEnableUnit(deps, spire.AgentUnit)
}
