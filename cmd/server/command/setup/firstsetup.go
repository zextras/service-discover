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
	"github.com/zextras/service-discover/pkg/encrypter"
	exec2 "github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/systemd"
)

var testingMode bool

// firstSetup specifically handles the command sent by the final user in a non-interactive way. This will print
// as less as possible and it is intended to be used in with power users or from other programs.
func (s *Setup) firstSetup(d businessDependencies) (formatter.Formatter, error) {
	networks, err := command.NonLoopbackInterfaces(d)
	if err != nil {
		return nil, err
	}

	if err := command.CheckValidBindingAddress(d, networks, s.BindAddress); err != nil {
		return nil, err
	}

	err = s.performSetup(d, &setupConfiguration{
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
func (s *Setup) performSetup(d businessDependencies, inputs *setupConfiguration) error {
	zimbraLocalConfig, err := carbonio.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return err
	}

	ldapHandler := d.LdapHandler(zimbraLocalConfig)
	zimbraHostname, err := command.RetrieveZimbraHostname(zimbraLocalConfig, ldapHandler)

	if err != nil {
		return err
	}

	err = command.CheckHostnameAddress(d, zimbraHostname)
	if err != nil {
		return err
	}

	err = s.generateCertificationAuthority(d)
	if err != nil {
		return err
	}

	gossipKey, err := generateGossipKey()
	if err != nil {
		return err
	}

	consulConfigFile, err := s.generateCertificateAndConfig(d, zimbraHostname, gossipKey)
	if err != nil {
		return err
	}

	consulFileBytes, err := json.MarshalIndent(consulConfigFile, "", "  ")

	if err != nil {
		return err
	}

	if err := os.WriteFile(s.ConsulFileConfig, consulFileBytes, os.FileMode(0600)); err != nil {
		return errors.Errorf("unable to save generated configuration file in %s: %s", s.ConsulHome, err)
	}

	err = permissions.SetStrictPermissions(d, s.ConsulFileConfig)
	if err != nil {
		return err
	}

	if err := command.SaveBindAddressConfiguration(s.MutableConfigFile, inputs.BindAddress); err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(d, s.MutableConfigFile)
	if err != nil {
		return err
	}

	if err := command.AddServiceInLDAP(ldapHandler, zimbraHostname); err != nil {
		return err
	}

	isContainer := command.CheckDockerContainer()

	if isContainer && !testingMode {
		cmd := exec.Command("service-discoverd-docker", "server")

		err = cmd.Run()
		if err != nil {
			return errors.WithMessage(err, "unable to start service-discoverd server")
		}
	} else {
		if err := systemd.StartSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit); err != nil {
			return errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
		}
	}

	aclBootstrapJSON, err := s.createACLBootstrapToken(d)
	if err != nil {
		return err
	}

	aclBootstrapUnmarshal := &command.ACLTokenCreation{}
	err = json.Unmarshal(aclBootstrapJSON, aclBootstrapUnmarshal)

	if err != nil {
		return errors.WithMessage(
			err,
			"unable to decode ACL bootstrap response from Consul",
		)
	}

	serverToken, err := command.CreateACLToken(
		d.CreateCommand,
		command.Server,
		zimbraHostname,
		aclBootstrapUnmarshal.SecretID,
	)

	if err != nil {
		return err
	}

	if err := command.SetACLToken(d.CreateCommand, serverToken, aclBootstrapUnmarshal.SecretID); err != nil {
		return err
	}

	aclFile, err := os.CreateTemp("", command.ConsulACLBootstrap)
	if err != nil {
		return err
	}

	defer os.Remove(aclFile.Name())

	if err = os.WriteFile(aclFile.Name(), aclBootstrapJSON, 0600); err != nil {
		return err
	}

	gossipKeyFile, err := os.CreateTemp("", command.GossipKey)
	if err != nil {
		return err
	}

	defer os.Remove(gossipKeyFile.Name())

	if err = os.WriteFile(gossipKeyFile.Name(), []byte(consulConfigFile.Encrypt), 0600); err != nil {
		return err
	}

	filesToCompress := map[string]string{
		command.GossipKey:                        gossipKeyFile.Name(),
		command.ConsulACLBootstrap:               aclFile.Name(),
		s.ConsulHome + "/" + command.ConsulCA:    s.ConsulHome + "/" + command.ConsulCA,
		s.ConsulHome + "/" + command.ConsulCAKey: s.ConsulHome + "/" + command.ConsulCAKey,
	}

	if err = s.createEncryptedSecret(filesToCompress, inputs.Password); err != nil {
		return err
	}

	if err = os.WriteFile(s.ConsulHome+"/password", []byte(s.Password), 0400); err != nil {
		return err
	}

	err = command.UploadCredentialsToLDAP(ldapHandler, s.ClusterCredential)
	if err != nil {
		return errors.WithMessage(err, "unable to upload credentials file to LDAP")
	}

	if !isContainer {
		err = systemd.EnableSystemdUnit(d.SystemdUnitHandler, serviceDiscoverUnit)
		if err != nil {
			return errors.Errorf("unable to enable %s unit: %s", serviceDiscoverUnit, err)
		}
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

	if err := encryptedSecretFiles.Chmod(os.FileMode(0600)); err != nil {
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

		if err = encWriter.AddFile(bufio.NewReader(file), stat, name, "/"); err != nil {
			return errors.Errorf("error while creating secret credentials: unable to include %s: %s", path, err)
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
			<-ticker.C

			aclBootstrapJSON, err := d.CreateCommand(consulBin, "acl", "bootstrap", "-format", "json").Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
					stderr := strings.TrimSpace(string(ee.Stderr))
					if stderr != "Failed ACL bootstrapping: Unexpected response code: 500 (The ACL system is currently in legacy mode.)" {
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

func (s *Setup) generateCertificationAuthority(d businessDependencies) error {
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
		return exec2.ErrorFromStderr(err, "unable to create a valid CA with Consul")
	}

	err = permissions.SetStrictPermissions(d, s.ConsulHome+"/consul-agent-ca-key.pem")
	if err != nil {
		return err
	}

	err = permissions.SetStrictPermissions(d, s.ConsulHome+"/consul-agent-ca.pem")
	if err != nil {
		return err
	}

	return nil
}
