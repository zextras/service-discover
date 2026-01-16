// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/systemd"
)

// importSetup refers to the run performed on a non-first cluster instance in a non-interactive way.
// The output returned is always empty
//
//nolint:misspell
func (s *Setup) importSetup(deps businessDependencies) (formatter.Formatter, error) {
	zimbraHostname, ldapHandler, err := s.loadConfigAndValidate(deps)
	if err != nil {
		return nil, err
	}

	extractedFiles, err := s.extractAndWriteCertificates(deps, ldapHandler)
	if err != nil {
		return nil, err
	}

	err = s.generateAndWriteConfig(deps, zimbraHostname, extractedFiles)
	if err != nil {
		return nil, err
	}

	err = command.AddServiceInLDAP(ldapHandler, zimbraHostname)
	if err != nil {
		return nil, err
	}

	isContainer, err := s.startServerAndSetupACL(deps, zimbraHostname, extractedFiles)
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

func (s *Setup) loadConfigAndValidate(deps businessDependencies) (string, carbonio.LdapHandler, error) {
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

	err = command.CheckHostnameAddress(deps, zimbraHostname)
	if err != nil {
		return "", nil, err
	}

	return zimbraHostname, ldapHandler, nil
}

func (s *Setup) extractAndWriteCertificates(
	deps businessDependencies,
	ldapHandler carbonio.LdapHandler,
) (map[string][]byte, error) {
	err := command.DownloadCredentialsFromLDAP(ldapHandler, s.ClusterCredential)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to download credentials from LDAP")
	}

	clusterCredential, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return nil, err
	}

	credReader, err := encrypter.NewReader(clusterCredential, []byte(s.Password))
	if err != nil {
		return nil, errors.Errorf("unable to open %s: %s", clusterCredential.Name(), err)
	}

	// We calculate the path relative to the root (i.e. without the "/" at the beginning) since this should not be
	// included in standard tarballs
	tarballCaFullPath, err := filepath.Rel("/", filepath.Join(config.ConsulHome, command.ConsulCA))
	if err != nil {
		return nil, err
	}

	localCaFullPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCA))
	if err != nil {
		return nil, err
	}

	tarballCaKeyFullPath, err := filepath.Rel("/", filepath.Join(config.ConsulHome, command.ConsulCAKey))
	if err != nil {
		return nil, err
	}

	localCaKeyFullPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCAKey))
	if err != nil {
		return nil, err
	}

	extractedFiles, err := encrypter.ReadFiles(
		credReader,
		tarballCaFullPath,
		tarballCaKeyFullPath,
		command.ConsulACLBootstrap,
		command.GossipKey,
	)
	if err != nil {
		return nil, err
	}

	err = os.WriteFile("/"+localCaFullPath, extractedFiles[tarballCaFullPath], os.FileMode(0600))
	if err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(deps, "/"+localCaFullPath)
	if err != nil {
		return nil, err
	}

	err = os.WriteFile("/"+localCaKeyFullPath, extractedFiles[tarballCaKeyFullPath], os.FileMode(0600))
	if err != nil {
		return nil, err
	}

	defer os.Remove("/" + localCaKeyFullPath)

	return extractedFiles, nil
}

func (s *Setup) generateAndWriteConfig(
	deps businessDependencies,
	zimbraHostname string,
	extractedFiles map[string][]byte,
) error {
	gossipKey := string(extractedFiles[command.GossipKey])

	consulConfigFile, err := s.generateCertificateAndConfig(deps, zimbraHostname, gossipKey)
	if err != nil {
		return err
	}

	consulFileBytes, err := json.MarshalIndent(consulConfigFile, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(s.ConsulFileConfig, consulFileBytes, os.FileMode(0600))
	if err != nil {
		return errors.Errorf("unable to save generated configuration file in %s: %s", s.ConsulHome, err)
	}

	err = permissions.SetStrictPermissions(deps, s.ConsulFileConfig)
	if err != nil {
		return err
	}

	err = command.SaveBindAddressConfiguration(s.MutableConfigFile, s.BindAddress)
	if err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(deps, s.MutableConfigFile)
	if err != nil {
		return err
	}

	return nil
}

func (s *Setup) startServerAndSetupACL(
	deps businessDependencies,
	zimbraHostname string,
	extractedFiles map[string][]byte,
) (bool, error) {
	isContainer := command.CheckDockerContainer()

	if isContainer && !testingMode {
		cmd := exec.CommandContext(context.Background(), "service-discoverd-docker", "server")

		err := cmd.Run()
		if err != nil {
			return isContainer, errors.WithMessage(err, "unable to start service-discoverd server")
		}
	} else {
		err := systemd.StartSystemdUnit(deps.SystemdUnitHandler, serviceDiscoverUnit)
		if err != nil {
			return isContainer, errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
		}
	}

	aclBootstrapToken := command.ACLTokenCreation{}

	err := json.Unmarshal(extractedFiles[command.ConsulACLBootstrap], &aclBootstrapToken)
	if err != nil {
		return isContainer, errors.WithMessagef(err, "unable to decode ACL Bootstrap token")
	}

	token, err := command.CreateACLToken(
		deps.CreateCommand,
		command.Server,
		zimbraHostname,
		aclBootstrapToken.SecretID,
	)
	if err != nil {
		return isContainer, errors.WithMessage(err, "unable to create ACL policy for this server")
	}

	err = command.SetACLToken(deps.CreateCommand, token, aclBootstrapToken.SecretID)
	if err != nil {
		return isContainer, err
	}

	return isContainer, nil
}
