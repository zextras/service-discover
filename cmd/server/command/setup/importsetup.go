// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/spire"
	"github.com/zextras/service-discover/pkg/systemd"
)

// importSetup refers to the run performed on a non-first cluster instance in a non-interactive way.
// The output returned is always empty
//
//nolint:misspell // Zimbra is the name of the product the localconfig belongs to
func (s *Setup) importSetup(deps businessDependencies) (formatter.Formatter, error) {
	networks, err := command.NonLoopbackInterfaces(deps)
	if err != nil {
		return nil, err
	}

	err = command.CheckValidBindingAddress(deps, networks, s.BindAddress)
	if err != nil {
		return nil, err
	}

	ldapHandler, zimbraHostname, err := s.resolveServerIdentity(deps)
	if err != nil {
		return nil, err
	}

	err = command.DownloadCredentialsFromLDAP(ldapHandler, s.ClusterCredential)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to download credentials from LDAP")
	}

	extractedFiles, caKeyPath, err := s.extractClusterCredentials(deps)
	if err != nil {
		return nil, err
	}

	defer os.Remove(caKeyPath)

	jwksURL, err := s.clusterJWKSURL()
	if err != nil {
		return nil, err
	}

	runSpire, err := s.importSpireServer(deps, zimbraHostname, jwksURL)
	if err != nil {
		return nil, err
	}

	err = s.writeConsulConfiguration(
		deps, zimbraHostname, string(extractedFiles[command.GossipKey]), jwksURL, s.BindAddress,
	)
	if err != nil {
		return nil, err
	}

	err = command.AddServiceInLDAP(ldapHandler, zimbraHostname)
	if err != nil {
		return nil, err
	}

	isContainer, err := startServiceDiscoverDaemon(deps)
	if err != nil {
		return nil, err
	}

	err = s.createServerToken(deps, zimbraHostname, extractedFiles[command.ConsulACLBootstrap])
	if err != nil {
		return nil, err
	}

	err = s.importSpireAgent(deps, zimbraHostname, runSpire)
	if err != nil {
		return nil, err
	}

	err = os.WriteFile(s.ConsulHome+"/password", []byte(s.Password), 0400)
	if err != nil {
		return nil, err
	}

	if !isContainer {
		err = systemd.EnableSystemdUnit(deps.SystemdUnitHandler, serviceDiscoverUnit)
		if err != nil {
			return nil, errors.Errorf("unable to enable %s unit: %s", serviceDiscoverUnit, err)
		}
	}

	return &formatter.EmptyFormatter{}, nil
}

// extractClusterCredentials reads the cluster credentials downloaded from LDAP and installs the cluster CA and its key
// in the Consul home. It returns the files read from the credentials, and the path of the CA key, that the caller
// must remove once the server certificates have been generated.
func (s *Setup) extractClusterCredentials(deps businessDependencies) (map[string][]byte, string, error) {
	clusterCredential, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return nil, "", err
	}

	credReader, err := encrypter.NewReader(clusterCredential, []byte(s.Password))
	if err != nil {
		return nil, "", errors.Errorf("unable to open %s: %s", clusterCredential.Name(), err)
	}
	// We calculate the path relative to the root (i.e. without the "/" at the beginning) since this should not be
	// included in standard tarballs
	tarballCaFullPath, err := filepath.Rel("/", filepath.Join(config.ConsulHome, command.ConsulCA))
	if err != nil {
		return nil, "", err
	}

	localCaFullPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCA))
	if err != nil {
		return nil, "", err
	}

	tarballCaKeyFullPath, err := filepath.Rel("/", filepath.Join(config.ConsulHome, command.ConsulCAKey))
	if err != nil {
		return nil, "", err
	}

	localCaKeyFullPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCAKey))
	if err != nil {
		return nil, "", err
	}

	extractedFiles, err := encrypter.ReadFiles(
		credReader,
		tarballCaFullPath,
		tarballCaKeyFullPath,
		command.ConsulACLBootstrap,
		command.GossipKey,
	)
	if err != nil {
		return nil, "", err
	}

	err = os.WriteFile("/"+localCaFullPath, extractedFiles[tarballCaFullPath], os.FileMode(0600))
	if err != nil {
		return nil, "", err
	}

	err = permissions.SetStrictPermissions(deps, "/"+localCaFullPath)
	if err != nil {
		return nil, "", err
	}

	err = os.WriteFile("/"+localCaKeyFullPath, extractedFiles[tarballCaKeyFullPath], os.FileMode(0600))
	if err != nil {
		return nil, "", err
	}

	return extractedFiles, "/" + localCaKeyFullPath, nil
}

// createServerToken creates the ACL token of this server using the bootstrap token shared by the first server.
func (s *Setup) createServerToken(deps businessDependencies, hostname string, aclBootstrapJSON []byte) error {
	aclBootstrapToken := command.ACLTokenCreation{}

	err := json.Unmarshal(aclBootstrapJSON, &aclBootstrapToken)
	if err != nil {
		return errors.WithMessagef(err, "unable to decode ACL Bootstrap token")
	}

	token, err := command.CreateACLToken(deps.CreateCommand, command.Server, hostname, aclBootstrapToken.SecretID)
	if err != nil {
		return errors.WithMessage(err, "unable to create ACL policy for this server")
	}

	return command.SetACLToken(deps.CreateCommand, token, aclBootstrapToken.SecretID)
}

// clusterJWKSURL returns the URL where the Consul servers fetch the keys validating the JWT-SVIDs from, as recorded
// in the cluster credentials by the first server: the loopback URL when every server runs SPIRE (high availability),
// the URL of the first server otherwise. It is empty for clusters set up before SPIRE was introduced: their servers
// keep working, but cannot authorize agents joining with a SPIRE join bundle.
func (s *Setup) clusterJWKSURL() (string, error) {
	clusterCredential, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return "", err
	}

	defer clusterCredential.Close()

	credReader, err := encrypter.NewReader(clusterCredential, []byte(s.Password))
	if err != nil {
		return "", errors.Errorf("unable to open %s: %s", clusterCredential.Name(), err)
	}

	files, err := encrypter.ReadOptionalFiles(credReader, spire.ClusterJWKSURLFile)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(files[spire.ClusterJWKSURLFile])), nil
}

// importSpireMode tells whether this server has to run its own SPIRE server: in a high availability cluster (the keys
// are served on the loopback address) every Consul server does, on the shared datastore. In a cluster with a single
// SPIRE server, or without SPIRE, it does not.
func (s *Setup) importSpireMode(jwksURL string) (bool, error) {
	runSpire := jwksURL == spire.LocalJWKSURL

	switch {
	case runSpire && !s.highAvailability():
		return false, errors.New("the SPIRE servers of this cluster share a PostgreSQL datastore: " +
			"give its connection string with --spire-datastore-file")
	case !runSpire && s.highAvailability():
		return false, errors.New("this cluster runs a single SPIRE server, or none: " +
			"--spire-datastore-file cannot be used")
	}

	return runSpire, nil
}

// importSpireServer runs the SPIRE server of this Consul server when the cluster is in high availability. It tells
// whether SPIRE runs on this server.
func (s *Setup) importSpireServer(deps businessDependencies, hostname, jwksURL string) (bool, error) {
	runSpire, err := s.importSpireMode(jwksURL)
	if err != nil {
		return false, err
	}

	if runSpire {
		_, err = s.setupSpireServer(deps, hostname, s.BindAddress, true)
		if err != nil {
			return false, errors.WithMessage(err, "unable to set up the SPIRE server")
		}
	}

	return runSpire, nil
}

// importSpireAgent sets up the SPIRE agent of this server, once its SPIRE server runs.
func (s *Setup) importSpireAgent(deps businessDependencies, hostname string, runSpire bool) error {
	if !runSpire || !manageSpireUnits() {
		return nil
	}

	err := s.setupSpireAgent(deps, hostname, s.BindAddress)
	if err != nil {
		return errors.WithMessage(err, "unable to set up the SPIRE agent of this server")
	}

	return nil
}
