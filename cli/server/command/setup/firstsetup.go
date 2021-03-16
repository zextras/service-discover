package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bitbucket.org/zextras/service-discover/cli/server/util"
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"os/exec"
	"time"
)

// firstSetup specifically handles the command sent by the final user in a non-interactive way. This will print
// as less as possible and it is intended to be used in with power users or from other programs
func (s *Setup) firstSetup(d businessDependencies) (formatter.Formatter, error) {
	networks, err := nonLoopbackInterfaces(d)
	if err != nil {
		return nil, err
	}
	if err := checkValidBindingAddress(d, networks, s.BindAddress); err != nil {
		return nil, err
	}
	err = s.performSetup(d, &setupConfigurations{
		firstInstance: s.FirstInstance,
		Password:      s.Password,
		BindAddress:   s.BindAddress,
	})
	if err != nil {
		return nil, err
	}

	return &nonInteractiveOutput{
		EncFilepath: s.ClusterCredential,
		Password:    "",
	}, nil
}

// performSetup is the core of the setup procedure. It loads the Zimbra localconfig, retrieve the hostname of the Zimbra
// installation (that it doesn't mean it is the hostname of the machine), then it proceeds to generate the appropriate
// keys for the service discover to work in a secure way to finally write all the configuration in a PGP signed tarball
// archive
func (s *Setup) performSetup(d businessDependencies, inputs *setupConfigurations) error {
	zimbraLocalConfig, err := zimbra.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to read Zimbra local config: %s", err))
	}
	ldapHandler := d.LdapHandler(zimbraLocalConfig)
	zimbraHostname, err := s.retrieveZimbraHostname(zimbraLocalConfig, ldapHandler)
	if err != nil {
		return err
	}

	consulConfigFile, err := s.generateKeys(d, zimbraHostname)
	if err != nil {
		return err
	}
	consulFileBytes, _ := json.Marshal(consulConfigFile)
	err = ioutil.WriteFile(s.ConsulFileConfig, consulFileBytes, os.FileMode(0644))
	if err != nil {
		return errors.New("unable to save generated configuration file in " + s.ConsulHome)
	}

	if err := s.saveBindAddressConfiguration(inputs.BindAddress); err != nil {
		return err
	}

	if err := s.addServiceInLDAP(ldapHandler, zimbraHostname); err != nil {
		return err
	}
	aclBootstrapJson, err := s.createACLBootstrapToken(d)
	if err != nil {
		return err
	}
	aclFile, err := ioutil.TempFile("", ConsulAclBootstrap)
	if err != nil {
		return err
	}
	defer os.Remove(aclFile.Name())
	if err = ioutil.WriteFile(aclFile.Name(), aclBootstrapJson, 0644); err != nil {
		return err
	}
	filesToCompress := map[string]string{
		ConsulAclBootstrap:                        aclFile.Name(),
		s.ConsulHome + "/" + ConsulCAFile:         s.ConsulHome + "/" + ConsulCAFile,
		s.ConsulHome + "/" + ConsulCertificate:    s.ConsulHome + "/" + ConsulCertificate,
		s.ConsulHome + "/" + ConsulCertificateKey: s.ConsulHome + "/" + ConsulCertificateKey,
	}

	if err = s.createEncryptedSecret(filesToCompress, inputs.Password); err != nil {
		return err
	}

	return s.enableServiceDiscoverd(d)
}

// createEncryptedSecret takes the passed files as [destination in tarball]: current location and puts it in a
// PGP encrypted tar archive
func (s *Setup) createEncryptedSecret(filesToCompress map[string]string, password string) error {
	encryptedSecretFiles, err := os.Create(s.ClusterCredential)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to create %s: %s", s.ClusterCredential, err))
	}
	defer encryptedSecretFiles.Close()
	encWriter, err := credentialsEncrypter.NewWriter(encryptedSecretFiles, []byte(password))
	if err != nil {
		return err
	}
	defer encWriter.Close()
	for name, path := range filesToCompress {
		file, err := os.Open(path)
		if err != nil {
			return errors.New(fmt.Sprintf("unable to open %s: %s", path, err))
		}
		stat, err := file.Stat()
		if err != nil {
			return errors.New(fmt.Sprintf("unable to stat() provided %s: %s", file.Name(), err))
		}
		if err = encWriter.AddFile(bufio.NewReader(file), stat, name, "/"); err != nil {
			return errors.New(fmt.Sprintf("error while creating secret credentials: impossible to include %s: %s", path, err))
		}
	}

	return encWriter.Flush()
}

func (s *Setup) createACLBootstrapToken(d businessDependencies) ([]byte, error) {
	err := util.StartSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return nil, err
	}
	time.Sleep(time.Second * 12)
	// Ok so now service-discoverd is running, we can now create our acl bootstrap token and save it in our secure
	// tarball
	aclBootstrapJson, err := d.CreateCommand(consulBin, "acl", "bootstrap", "-format", "json").Output()
	if err != nil {
		reason := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			reason = string(ee.Stderr)
		}
		return nil, errors.New(fmt.Sprintf("unable to create ACL bootstrap token: %s", reason))
	}
	return aclBootstrapJson, nil
}

// generateKeys creates the TLS certificates for consul and finally it generates the gossip key. This ensure secure
// communications inside Consul
func (s *Setup) generateKeys(d businessDependencies, zimbraHostname string) (*setupConfig, error) {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
	err := s.execInPath(d, s.ConsulHome, consulBin, "tls", "ca", "create", certificateDaysFlag, "-name-constraint")
	if err != nil {
		reason := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			reason = string(ee.Stderr)
		}
		return nil, errors.New(fmt.Sprintf("unable to create a valid CA with Consul: %s", reason))
	}
	err = s.execInPath(d, s.ConsulHome, consulBin, "tls", "cert", "create", certificateDaysFlag, "-server")
	if err != nil {
		return nil, errors.New("unable to create a valid certificate with Consul")
	}
	gossipKey, err := generateGossipKey()
	if err != nil {
		return nil, err
	}

	consulConfigFile := &setupConfig{
		AclConfig: aclConfig{
			Enabled:                true,
			EnableTokenPersistence: true,
			DefaultPolicy:          "deny",
			DownPolicy:             "extend-cache",
		},
		CaFile:                  s.ConsulHome + "/" + ConsulCAFile,
		CertFile:                s.ConsulHome + "/" + ConsulCertificate,
		KeyFile:                 s.ConsulHome + "/" + ConsulCertificateKey,
		DataDir:                 s.ConsulData,
		LogLevel:                defaultLogLever,
		NodeName:                zimbraHostname,
		Encrypt:                 gossipKey,
		EnableLocalScriptChecks: true,
		AutoEncrypt:             autoEncrypt{true},
		Server:                  true,
		VerifyIncoming:          true,
		VerifyOutgoing:          true,
		VerifyServerHostname:    true,
	}
	return consulConfigFile, nil
}
