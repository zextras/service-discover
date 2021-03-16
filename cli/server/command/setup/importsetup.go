package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bitbucket.org/zextras/service-discover/cli/server/config"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

type nonInteractiveImportOutput struct {
}

func (n *nonInteractiveImportOutput) PlainRender() (string, error) {
	return "", nil
}

func (n *nonInteractiveImportOutput) JsonRender() (string, error) {
	return "{}", nil
}

// importSetup refers to the run performed on a non-first cluster instance in a non-interactive way.
// The output returned is always empty
func (s *Setup) importSetup(d businessDependencies) (formatter.Formatter, error) {
	clusterCredential, err := s.openClusterCredential()
	if err != nil {
		return nil, err
	}

	networks, err := nonLoopbackInterfaces(d)
	if err != nil {
		return nil, err
	}
	if err := checkValidBindingAddress(d, networks, s.BindAddress); err != nil {
		return nil, err
	}

	bsCaFile, err := s.extractNecessaryFilesFromArchive(clusterCredential, s.Password)
	if err != nil {
		return nil, err
	}
	caFile, err := ioutil.TempFile("", config.ApplicationName+"*")
	if err != nil {
		return nil, err
	}
	err = ioutil.WriteFile(caFile.Name(), bsCaFile, os.FileMode(0644))
	if err != nil {
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
	zimbraHostname, err := s.retrieveZimbraHostname(zimbraLocalConfig, ldapHandler)
	if err != nil {
		return nil, err
	}
	if err := s.saveBindAddressConfiguration(s.BindAddress); err != nil {
		return nil, err
	}
	if err := s.addServiceInLDAP(ldapHandler, zimbraHostname); err != nil {
		return nil, err
	}

	if err := s.enableServiceDiscoverd(d); err != nil {
		return nil, err
	}

	return &nonInteractiveImportOutput{}, nil
}

func (s *Setup) createTLSCertificate(d businessDependencies, caFile *os.File) error {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
	err := s.execInPath(
		d,
		s.ConsulHome,
		consulBin,
		"tls",
		"cert",
		"create",
		certificateDaysFlag,
		"-server",
		"-ca",
		caFile.Name(),
	)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to generate correct CA certificate: %s", err))
	}
	return nil
}

// extractNecessaryFilesFromArchive is a specific methods that returns the CAFile of the imported secret. Additionally,
// it check the inside the cluster credentials file there are all the necessary components in order to assure a smooth
// run
func (s *Setup) extractNecessaryFilesFromArchive(clusterCredential *os.File, password string) ([]byte, error) {
	bsCaFile := make([]byte, 0)
	tarReader, err := credentialsEncrypter.NewReader(clusterCredential, []byte(password))
	if err != nil {
		return nil, errors.New(fmt.Sprintf("unable to open %s: %s", s.ClusterCredential, err))
	}
	// Now we need to extract the following files from the archive and put them in the right paths:
	// - "consul-agent-ca.pem"
	// - "consul-agent-ca-key.pem"
	// - "consul-acl-secret.json"
	caFileNamePresent := false
	caKeyFileNamePresent := false
	aclBootstrapPresent := false
	if err != nil {
		return nil, err
	}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New(fmt.Sprintf("unable to read tar header: %s", err))
		}
		// We don't fill the buffer until we're sure this is one of the files we're searching for!
		switch filepath.Base(header.Name) {
		case ConsulAclBootstrap:
			aclBootstrapPresent = true
			break
		case ConsulCAFile:
			if bsCaFile, err = credentialsEncrypter.ReadFile(tarReader); err != nil {
				return nil, err
			}
			caFileNamePresent = true
			break
		case ConsulCertificateKey:
			caKeyFileNamePresent = true
			break
		}
	}

	if !caFileNamePresent || !caKeyFileNamePresent || !aclBootstrapPresent {
		return nil, errors.New("not all required files where detected in " + s.ClusterCredential)
	}
	return bsCaFile, nil
}

// openClusterCredential checks that the given path, s.ClusterCredential exists and it is readable
func (s *Setup) openClusterCredential() (*os.File, error) {
	clusterCredential, err := os.Open(s.ClusterCredential)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New(fmt.Sprintf(
				"cannot find Cluster credential in %s, please copy the file from the existing server",
				s.ClusterCredential,
			))
		} else {
			return nil, err
		}
	}
	return clusterCredential, nil
}
