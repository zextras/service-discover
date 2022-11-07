package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/permissions"
	"bitbucket.org/zextras/service-discover/cli/lib/systemd"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"path/filepath"
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

	zimbraLocalConfig, err := zimbra.LoadLocalConfig(s.LocalConfigPath)
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
	caFullPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCA))
	if err != nil {
		return nil, err
	}
	caKeyFullPath, err := filepath.Rel("/", filepath.Join(s.ConsulHome, command.ConsulCAKey))
	if err != nil {
		return nil, err
	}
	extractedFiles, err := credentialsEncrypter.ReadFiles(
		credReader,
		caFullPath,
		caKeyFullPath,
		command.ConsulAclBootstrap,
		command.GossipKey,
	)
	if err != nil {
		return nil, err
	}

	if err := ioutil.WriteFile("/"+caFullPath, extractedFiles[caFullPath], os.FileMode(0600)); err != nil {
		return nil, err
	}

	err = permissions.SetStrictPermissions(d, "/"+caFullPath)
	if err != nil {
		return nil, err
	}

	if err := ioutil.WriteFile("/"+caKeyFullPath, extractedFiles[caKeyFullPath], os.FileMode(0600)); err != nil {
		return nil, err
	}
	defer os.Remove("/" + caKeyFullPath)

	gossipKey := string(extractedFiles[command.GossipKey])
	consulConfigFile, err := s.generateCertificateAndConfig(d, zimbraHostname, gossipKey)
	if err != nil {
		return nil, err
	}
	consulFileBytes, err := json.MarshalIndent(consulConfigFile, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(s.ConsulFileConfig, consulFileBytes, os.FileMode(0600)); err != nil {
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

	if err := systemd.StartSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit); err != nil {
		return nil, errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
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

	if err = ioutil.WriteFile(s.ConsulHome+"/password", []byte(s.Password), 0400); err != nil {
		return nil, err
	}

	err = systemd.EnableSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("unable to enable %s unit: %s", serviceDiscoverUnit, err))
	}

	return &formatter.EmptyFormatter{}, nil
}
