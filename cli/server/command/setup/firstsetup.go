package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command/setup"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	exec2 "bitbucket.org/zextras/service-discover/cli/lib/exec"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/systemd"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"
)

// firstSetup specifically handles the command sent by the final user in a non-interactive way. This will print
// as less as possible and it is intended to be used in with power users or from other programs
func (s *Setup) firstSetup(d businessDependencies) (formatter.Formatter, error) {
	networks, err := setup.NonLoopbackInterfaces(d)
	if err != nil {
		return nil, err
	}
	if err := setup.CheckValidBindingAddress(d, networks, s.BindAddress); err != nil {
		return nil, err
	}
	err = s.performSetup(d, &setupConfiguration{
		firstInstance: s.firstInstance,
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
func (s *Setup) performSetup(d businessDependencies, inputs *setupConfiguration) error {
	zimbraLocalConfig, err := zimbra.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to read Zimbra local config: %s", err))
	}
	ldapHandler := d.LdapHandler(zimbraLocalConfig)
	zimbraHostname, err := setup.RetrieveZimbraHostname(zimbraLocalConfig, ldapHandler)
	if err != nil {
		return err
	}

	consulConfigFile, err := s.generateKeys(d, zimbraHostname)
	if err != nil {
		return err
	}
	consulFileBytes, err := json.MarshalIndent(consulConfigFile, "", "  ")
	if err != nil {
		return err
	}
	// FIXME the ownership of the file should be fixed! + 0600 perm should be used
	if err := ioutil.WriteFile(s.ConsulFileConfig, consulFileBytes, os.FileMode(0644)); err != nil {
		return errors.New(fmt.Sprintf("unable to save generated configuration file in %s: %s", s.ConsulHome, err))
	}

	if err := setup.SaveBindAddressConfiguration(s.MutableConfigFile, inputs.BindAddress); err != nil {
		return err
	}

	if err := setup.AddServiceInLDAP(ldapHandler, zimbraHostname); err != nil {
		return err
	}
	err = systemd.StartSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return err
	}
	aclBootstrapJson, err := s.createACLBootstrapToken(d)
	if err != nil {
		return err
	}
	aclBootstrapUnmarshal := &setup.ACLTokenCreation{}
	err = json.Unmarshal(aclBootstrapJson, aclBootstrapUnmarshal)
	if err != nil {
		return errors.WithMessage(
			err,
			"unable to decode ACL bootstrap response from Consul",
		)
	}
	serverToken, err := setup.CreateACLToken(
		d.CreateCommand,
		setup.Server,
		zimbraHostname,
		aclBootstrapUnmarshal.SecretID,
	)
	if err != nil {
		return err
	}
	if err := setup.SetACLToken(d.CreateCommand, serverToken, aclBootstrapUnmarshal.SecretID); err != nil {
		return err
	}
	aclFile, err := ioutil.TempFile("", setup.ConsulAclBootstrap)
	if err != nil {
		return err
	}
	defer os.Remove(aclFile.Name())
	if err = ioutil.WriteFile(aclFile.Name(), aclBootstrapJson, 0600); err != nil {
		return err
	}
	gossipKeyFile, err := ioutil.TempFile("", setup.GossipKey)
	if err != nil {
		return err
	}
	defer os.Remove(gossipKeyFile.Name())
	if err = ioutil.WriteFile(gossipKeyFile.Name(), []byte(consulConfigFile.Encrypt), 0600); err != nil {
		return err
	}
	filesToCompress := map[string]string{
		setup.GossipKey:                        gossipKeyFile.Name(),
		setup.ConsulAclBootstrap:               aclFile.Name(),
		s.ConsulHome + "/" + setup.ConsulCA:    s.ConsulHome + "/" + setup.ConsulCA,
		s.ConsulHome + "/" + setup.ConsulCAKey: s.ConsulHome + "/" + setup.ConsulCAKey,
	}

	if err = s.createEncryptedSecret(filesToCompress, inputs.Password); err != nil {
		return err
	}

	err = systemd.EnableSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to enable %s unit: %s", serviceDiscoverUnit, err))
	}
	return nil
}

// createEncryptedSecret takes the passed files as [destination in tarball]: current location and puts it in a
// PGP encrypted tar archive
func (s *Setup) createEncryptedSecret(filesToCompress map[string]string, password string) error {
	encryptedSecretFiles, err := os.Create(s.ClusterCredential)
	if err != nil {
		return errors.New(fmt.Sprintf("unable to create %s: %s", s.ClusterCredential, err))
	}
	defer encryptedSecretFiles.Close()
	if err := encryptedSecretFiles.Chmod(os.FileMode(0600)); err != nil {
		return errors.New(fmt.Sprintf("unable to change permission to %s: %s", s.ClusterCredential, err))
	}
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
	type returnResult struct {
		data []byte
		err  error
	}
	result := make(chan returnResult, 1)
	ticker := time.NewTicker(250 * time.Millisecond)
	go func() {
		for {
			select {
			case <-ticker.C:
				aclBootstrapJson, err := d.CreateCommand(consulBin, "acl", "bootstrap", "-format", "json").Output()
				if err != nil {
					stderr := err.Error()
					if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
						stderr = strings.TrimSpace(string(ee.Stderr))
						if stderr != "Failed ACL bootstrapping: Unexpected response code: 500 (The ACL system is currently in legacy mode.)" {
							res := returnResult{err: exec2.ErrorFromStderr(err, "unable to create ACL bootstrap token")}
							result <- res
							return
						}
					}
				} else {
					res := returnResult{data: aclBootstrapJson}
					result <- res
					return
				}
			}
		}
	}()

	select {
	case res := <-result:
		ticker.Stop() // note: we don't really need this since the goroutine at this point should already be exited
		return res.data, res.err
	case <-time.After(time.Second * 30):
		ticker.Stop()
		return nil, errors.New("timeout reached while waiting for consul to be ready")
	}
}

// generateKeys creates the TLS certificates for consul and finally it generates the gossip key. This ensure secure
// communications inside Consul
func (s *Setup) generateKeys(d businessDependencies, zimbraHostname string) (*setupConfig, error) {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
	err := exec2.InPath(
		d.CreateCommand(consulBin,
			"tls",
			"ca",
			"create",
			certificateDaysFlag,
			"-name-constraint"),
		s.ConsulHome,
	)
	if err != nil {
		return nil, exec2.ErrorFromStderr(err, "unable to create a valid CA with Consul")
	}
	err = exec2.InPath(
		d.CreateCommand(consulBin,
			"tls",
			"cert",
			"create",
			certificateDaysFlag,
			"-server"),
		s.ConsulHome,
	)
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
		AutoEncrypt:             autoEncrypt{AllowTLS: true},
		CaFile:                  s.ConsulHome + "/" + setup.ConsulCA,
		CertFile:                s.ConsulHome + "/" + setup.ConsulServerCertificate,
		DataDir:                 s.ConsulData,
		EnableLocalScriptChecks: true,
		Encrypt:                 gossipKey,
		KeyFile:                 s.ConsulHome + "/" + setup.ConsulServerCertificateKey,
		LogLevel:                defaultLogLevel,
		NodeName:                setup.ConsulNodeName(setup.Server, zimbraHostname),
		Server:                  true,
		VerifyIncoming:          true,
		VerifyOutgoing:          true,
		VerifyServerHostname:    true,
		UiConfig:                uiConfig{Enabled: true},
		Ports:                   portsConfig{Grpc: 8502},
		Connect:                 connectConfig{Enabled: true},
	}
	return consulConfigFile, nil
}
