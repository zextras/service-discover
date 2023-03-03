package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Zextras/service-discover/cli/lib/carbonio"
	"github.com/Zextras/service-discover/cli/lib/command"
	"github.com/Zextras/service-discover/cli/lib/credentialsEncrypter"
	"github.com/Zextras/service-discover/cli/lib/formatter"
	"github.com/Zextras/service-discover/cli/lib/permissions"
	"github.com/Zextras/service-discover/cli/lib/systemd"
	"github.com/Zextras/service-discover/cli/server/config"
	"github.com/pkg/errors"
)

// importSetup refers to the run performed on a non-first cluster instance in a non-interactive way.
// The output returned is always empty
func (s *Setup) importSetup(d businessDependencies) (formatter.Formatter, error) {
	networks, err := command.NonLoopbackInterfaces(d)
	if err != nil {
		return nil, err
	}
	if err := command.CheckValidBindingAddress(d, networks, s.BindAddress); err != nil {
		return nil, err
	}

	zimbraLocalConfig, err := carbonio.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return nil, err
	}

	ldapHandler := d.LdapHandler(zimbraLocalConfig)
	zimbraHostname, err := command.RetrieveZimbraHostname(zimbraLocalConfig, ldapHandler)
	if err != nil {
		return nil, err
	}
	err = command.CheckHostnameAddress(d, zimbraHostname)
	if err != nil {
		return nil, err
	}
	if err := command.DownloadCredentialsFromLDAP(ldapHandler, s.ClusterCredential); err != nil {
		return nil, errors.WithMessage(err, "unable to download credentials from LDAP")
	}
	clusterCredential, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return nil, err
	}

	credReader, err := credentialsEncrypter.NewReader(clusterCredential, []byte(s.Password))
	if err != nil {
		return nil, errors.New(fmt.Sprintf("unable to open %s: %s", clusterCredential.Name(), err))
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
	extractedFiles, err := credentialsEncrypter.ReadFiles(
		credReader,
		tarballCaFullPath,
		tarballCaKeyFullPath,
		command.ConsulAclBootstrap,
		command.GossipKey,
	)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile("/"+localCaFullPath, extractedFiles[tarballCaFullPath], os.FileMode(0600)); err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(d, "/"+localCaFullPath)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile("/"+localCaKeyFullPath, extractedFiles[tarballCaKeyFullPath], os.FileMode(0600)); err != nil {
		return nil, err
	}
	defer os.Remove("/" + localCaKeyFullPath)

	gossipKey := string(extractedFiles[command.GossipKey])
	consulConfigFile, err := s.generateCertificateAndConfig(d, zimbraHostname, gossipKey)
	if err != nil {
		return nil, err
	}
	consulFileBytes, err := json.MarshalIndent(consulConfigFile, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.ConsulFileConfig, consulFileBytes, os.FileMode(0600)); err != nil {
		return nil, errors.New(fmt.Sprintf("unable to save generated configuration file in %s: %s", s.ConsulHome, err))
	}

	err = permissions.SetStrictPermissions(d, s.ConsulFileConfig)
	if err != nil {
		return nil, err
	}

	if err := command.SaveBindAddressConfiguration(s.MutableConfigFile, s.BindAddress); err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(d, s.MutableConfigFile)
	if err != nil {
		return nil, err
	}

	if err := command.AddServiceInLDAP(ldapHandler, zimbraHostname); err != nil {
		return nil, err
	}

	isContainer := command.CheckDockerContainer()

	if isContainer && !testingMode {
		cmd := exec.Command("sudo", "-u", "service-discover",
			"service-discoverd-docker", "server")

		err = cmd.Run()
		if err != nil {
			return nil, errors.WithMessage(err, "unable to start service-discoverd server")
		}
	} else {
		if err := systemd.StartSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit); err != nil {
			return nil, errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
		}
	}

	aclBootstrapToken := command.ACLTokenCreation{}
	if err := json.Unmarshal(extractedFiles[command.ConsulAclBootstrap], &aclBootstrapToken); err != nil {
		return nil, errors.WithMessagef(err, "unable to decode ACL Bootstrap token")
	}

	token, err := command.CreateACLToken(
		d.CreateCommand,
		command.Server,
		zimbraHostname,
		aclBootstrapToken.SecretID,
	)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to create ACL policy for this server")
	}
	err = command.SetACLToken(d.CreateCommand, token, aclBootstrapToken.SecretID)
	if err != nil {
		return nil, err
	}

	if err = os.WriteFile(s.ConsulHome+"/password", []byte(s.Password), 0400); err != nil {
		return nil, err
	}

	if !isContainer {
		err = systemd.EnableSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("unable to enable %s unit: %s", serviceDiscoverUnit, err))
		}
	}

	return &formatter.EmptyFormatter{}, nil
}
