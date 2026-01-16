// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	sharedsetup "github.com/zextras/service-discover/pkg/command/setup"
	"github.com/zextras/service-discover/pkg/encrypter"
	exec2 "github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
)

// firstSetup specifically handles the command sent by the final user in a non-interactive way. This will print
// as less as possible and it is intended to be used in with power users or from other programs.
func (s *Setup) firstSetup(deps sharedsetup.BusinessDependencies) (formatter.Formatter, error) {
	networks, err := command.NonLoopbackInterfaces(deps)
	if err != nil {
		return nil, err
	}

	err = command.CheckValidBindingAddress(deps, networks, s.BindAddress)
	if err != nil {
		return nil, err
	}

	err = s.performSetup(deps, &setupConfiguration{
		FirstInstance: s.FirstInstance,
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
//
//nolint:misspell
func (s *Setup) performSetup(deps sharedsetup.BusinessDependencies, inputs *setupConfiguration) error {
	zimbraHostname, ldapHandler, consulConfigFile, _, err := s.setupConfigAndKeys(deps)
	if err != nil {
		return err
	}

	err = s.writeConsulConfiguration(deps, consulConfigFile, inputs.BindAddress)
	if err != nil {
		return err
	}

	err = command.AddServiceInLDAP(ldapHandler, zimbraHostname)
	if err != nil {
		return err
	}

	isContainer, err := s.startConsulServer(deps)
	if err != nil {
		return err
	}

	aclBootstrapJSON, err := s.setupACLAndTokens(deps, zimbraHostname)
	if err != nil {
		return err
	}

	err = s.createAndUploadSecrets(ldapHandler, consulConfigFile, aclBootstrapJSON, inputs.Password)
	if err != nil {
		return err
	}

	return sharedsetup.EnableSystemdUnitIfNotContainer(deps, isContainer)
}

func (s *Setup) setupConfigAndKeys(
	deps sharedsetup.BusinessDependencies,
) (string, carbonio.LdapHandler, *setupConfig, string, error) {
	zimbraLocalConfig, err := carbonio.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return "", nil, nil, "", err
	}

	ldapHandler := deps.LdapHandler(zimbraLocalConfig)

	zimbraHostname, err := command.RetrieveZimbraHostname(zimbraLocalConfig, ldapHandler)
	if err != nil {
		return "", nil, nil, "", err
	}

	err = command.CheckHostnameAddress(deps, zimbraHostname)
	if err != nil {
		return "", nil, nil, "", err
	}

	err = s.generateCertificationAuthority(deps)
	if err != nil {
		return "", nil, nil, "", err
	}

	gossipKey, err := generateGossipKey()
	if err != nil {
		return "", nil, nil, "", err
	}

	consulConfigFile, err := s.generateCertificateAndConfig(deps, zimbraHostname, gossipKey)
	if err != nil {
		return "", nil, nil, "", err
	}

	return zimbraHostname, ldapHandler, consulConfigFile, gossipKey, nil
}

func (s *Setup) writeConsulConfiguration(
	deps sharedsetup.BusinessDependencies,
	consulConfigFile *setupConfig,
	bindAddress string,
) error {
	consulFileBytes, err := json.MarshalIndent(consulConfigFile, "", "  ")
	if err != nil {
		return err
	}

	err = sharedsetup.WriteFileWithStrictPermissions(deps, s.ConsulFileConfig, consulFileBytes, os.FileMode(0600))
	if err != nil {
		return errors.Errorf("unable to save generated configuration file in %s: %s", s.ConsulHome, err)
	}

	return sharedsetup.SaveBindAddressWithPermissions(deps, s.MutableConfigFile, bindAddress)
}

func (s *Setup) startConsulServer(deps sharedsetup.BusinessDependencies) (bool, error) {
	return sharedsetup.StartServiceDiscoverMode(deps, "server")
}

func (s *Setup) setupACLAndTokens(deps sharedsetup.BusinessDependencies, zimbraHostname string) ([]byte, error) {
	aclBootstrapJSON, err := s.createACLBootstrapToken(deps)
	if err != nil {
		return nil, err
	}

	aclBootstrapUnmarshal := &command.ACLTokenCreation{}

	err = json.Unmarshal(aclBootstrapJSON, aclBootstrapUnmarshal)
	if err != nil {
		return nil, errors.WithMessage(
			err,
			"unable to decode ACL bootstrap response from Consul",
		)
	}

	serverToken, err := command.CreateACLToken(
		deps.CreateCommand,
		command.Server,
		zimbraHostname,
		aclBootstrapUnmarshal.SecretID,
	)
	if err != nil {
		return nil, err
	}

	err = command.SetACLToken(deps.CreateCommand, serverToken, aclBootstrapUnmarshal.SecretID)
	if err != nil {
		return nil, err
	}

	return aclBootstrapJSON, nil
}

func (s *Setup) createAndUploadSecrets(
	ldapHandler carbonio.LdapHandler,
	consulConfigFile *setupConfig,
	aclBootstrapJSON []byte,
	password string,
) error {
	aclFile, err := os.CreateTemp("", command.ConsulACLBootstrap)
	if err != nil {
		return err
	}

	defer os.Remove(aclFile.Name())

	err = os.WriteFile(aclFile.Name(), aclBootstrapJSON, 0600)
	if err != nil {
		return err
	}

	gossipKeyFile, err := os.CreateTemp("", command.GossipKey)
	if err != nil {
		return err
	}

	defer os.Remove(gossipKeyFile.Name())

	err = os.WriteFile(gossipKeyFile.Name(), []byte(consulConfigFile.Encrypt), 0600)
	if err != nil {
		return err
	}

	filesToCompress := map[string]string{
		command.GossipKey:                        gossipKeyFile.Name(),
		command.ConsulACLBootstrap:               aclFile.Name(),
		s.ConsulHome + "/" + command.ConsulCA:    s.ConsulHome + "/" + command.ConsulCA,
		s.ConsulHome + "/" + command.ConsulCAKey: s.ConsulHome + "/" + command.ConsulCAKey,
	}

	err = s.createEncryptedSecret(filesToCompress, password)
	if err != nil {
		return err
	}

	err = writePasswordFile(s.ConsulHome, s.Password)
	if err != nil {
		return err
	}

	err = command.UploadCredentialsToLDAP(ldapHandler, s.ClusterCredential)
	if err != nil {
		return errors.WithMessage(err, "unable to upload credentials file to LDAP")
	}

	return nil
}

// createEncryptedSecret takes the passed files as [destination in tarball]: current location and puts it in a
// PGP encrypted tar archive.
func (s *Setup) createEncryptedSecret(filesToCompress map[string]string, password string) error {
	encryptedSecretFiles, err := os.Create(s.ClusterCredential)
	if err != nil {
		return errors.Errorf("unable to create %s: %s", s.ClusterCredential, err)
	}

	defer func(encryptedSecretFiles *os.File) {
		_ = encryptedSecretFiles.Close()
	}(encryptedSecretFiles)

	err = encryptedSecretFiles.Chmod(os.FileMode(0600))
	if err != nil {
		return errors.Errorf("unable to change permission to %s: %s", s.ClusterCredential, err)
	}

	encWriter, err := encrypter.NewWriter(encryptedSecretFiles, []byte(password))
	if err != nil {
		return err
	}

	defer encWriter.Close()

	for name, path := range filesToCompress {
		file, err := os.Open(path) // #nosec
		if err != nil {
			return errors.Errorf("unable to open %s: %s", path, err)
		}

		stat, err := file.Stat()
		if err != nil {
			return errors.Errorf("unable to stat() provided %s: %s", file.Name(), err)
		}

		err = encWriter.AddFile(bufio.NewReader(file), stat, name, "/")
		if err != nil {
			return errors.Errorf("error while creating secret credentials: unable to include %s: %s", path, err)
		}
	}

	return encWriter.Flush()
}

func (s *Setup) createACLBootstrapToken(deps sharedsetup.BusinessDependencies) ([]byte, error) {
	type returnResult struct {
		data []byte
		err  error
	}

	result := make(chan returnResult, 1)
	ticker := time.NewTicker(250 * time.Millisecond)

	go func() {
		for {
			<-ticker.C

			aclBootstrapJSON, err := deps.CreateCommand(consulBin, "acl", "bootstrap", "-format", "json").Output()
			if err != nil {
				ee := &exec.ExitError{}
				if errors.As(err, &ee) {
					stderr := strings.TrimSpace(string(ee.Stderr))

					expectedLegacyModeErr := "Failed ACL bootstrapping: Unexpected response code: " +
						"500 (The ACL system is currently in legacy mode.)"
					if stderr != expectedLegacyModeErr {
						res := returnResult{err: exec2.ErrorFromStderr(err, "unable to create ACL bootstrap token")}
						result <- res

						return
					}
				}
			} else {
				res := returnResult{data: aclBootstrapJSON}
				result <- res

				return
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

func (s *Setup) generateCertificationAuthority(deps sharedsetup.BusinessDependencies) error {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)

	err := exec2.InPath(
		deps.CreateCommand(consulBin,
			"tls",
			"ca",
			"create",
			certificateDaysFlag,
			"-name-constraint"),
		s.ConsulHome,
	)
	if err != nil {
		return exec2.ErrorFromStderr(err, "unable to create a valid CA with Consul")
	}

	err = permissions.SetStrictPermissions(deps, s.ConsulHome+"/consul-agent-ca-key.pem")
	if err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(deps, s.ConsulHome+"/consul-agent-ca.pem")
	if err != nil {
		return err
	}

	return nil
}
