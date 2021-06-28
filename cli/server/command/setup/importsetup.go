package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	"bitbucket.org/zextras/service-discover/cli/lib/exec"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/systemd"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bitbucket.org/zextras/service-discover/cli/server/config"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"path/filepath"
)

// importSetup refers to the run performed on a non-first cluster instance in a non-interactive way.
// The output returned is always empty
func (s *Setup) importSetup(d businessDependencies) (formatter.Formatter, error) {
	clusterCredential, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return nil, err
	}

	networks, err := command.NonLoopbackInterfaces(d)
	if err != nil {
		return nil, err
	}
	if err := command.CheckValidBindingAddress(d, networks, s.BindAddress); err != nil {
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
	caFileInMap, err := credentialsEncrypter.ReadFiles(credReader, caFullPath)
	if err != nil {
		return nil, err
	}
	caFile, err := ioutil.TempFile("", config.ApplicationName+"*")
	if err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(caFile.Name(), caFileInMap[caFullPath], os.FileMode(0644)); err != nil {
		return nil, err
	}
	defer os.Remove(caFile.Name())

	err = s.createTLSCertificate(d, caFile)
	if err != nil {
		return nil, err
	}

	zimbraLocalConfig, err := zimbra.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return nil, errors.New("unable to read Zimbra local config")
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
	if err := command.SaveBindAddressConfiguration(s.MutableConfigFile, s.BindAddress); err != nil {
		return nil, err
	}
	if err := command.AddServiceInLDAP(ldapHandler, zimbraHostname); err != nil {
		return nil, err
	}

	err = systemd.EnableSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("unable to enable %s unit: %s", serviceDiscoverUnit, err))
	}

	return &formatter.EmptyFormatter{}, nil
}

func (s *Setup) createTLSCertificate(d businessDependencies, caFile *os.File) error {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
	err := exec.InPath(
		d.CreateCommand(consulBin,
			"tls",
			"cert",
			"create",
			certificateDaysFlag,
			"-server",
			"-ca",
			caFile.Name()),
		s.ConsulHome,
	)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to generate correct CA certificate: %s", err))
	}
	return nil
}
