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
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/encrypter"
	exec2 "github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/spire"
	"github.com/zextras/service-discover/pkg/systemd"
)

var testingMode bool

// consulLeaderPollInterval and consulLeaderTimeout drive waitForConsulLeader. With the default raft_multiplier Consul
// randomizes the election timeout between 5 and 10 seconds, so the timeout leaves plenty of margin.
var (
	consulLeaderPollInterval = 250 * time.Millisecond
	consulLeaderTimeout      = 60 * time.Second
)

// firstSetup specifically handles the command sent by the final user in a non-interactive way. This will print
// as less as possible and it is intended to be used in with power users or from other programs.
func (s *Setup) firstSetup(deps businessDependencies) (formatter.Formatter, error) {
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
//nolint:misspell // Zimbra is the name of the product the localconfig belongs to
func (s *Setup) performSetup(deps businessDependencies, inputs *setupConfiguration) error {
	ldapHandler, zimbraHostname, err := s.resolveServerIdentity(deps)
	if err != nil {
		return err
	}

	err = s.generateCertificationAuthority(deps, zimbraHostname)
	if err != nil {
		return err
	}

	oidcURL, err := s.setupSpireServer(deps, zimbraHostname, inputs.BindAddress)
	if err != nil {
		return errors.WithMessage(err, "unable to set up the SPIRE server")
	}

	gossipKey, err := generateGossipKey()
	if err != nil {
		return err
	}

	err = s.writeConsulConfiguration(deps, zimbraHostname, gossipKey, oidcURL, inputs.BindAddress)
	if err != nil {
		return err
	}

	err = command.AddServiceInLDAP(ldapHandler, zimbraHostname)
	if err != nil {
		return err
	}

	isContainer, err := startServiceDiscover(deps)
	if err != nil {
		return err
	}

	aclBootstrapJSON, rootToken, err := s.bootstrapACL(deps, zimbraHostname)
	if err != nil {
		return err
	}

	err = s.setupSpireIntegration(deps, zimbraHostname, inputs.BindAddress, oidcURL, rootToken)
	if err != nil {
		return err
	}

	err = s.createClusterCredentials(clusterSecrets{
		aclBootstrapJSON: aclBootstrapJSON,
		gossipKey:        gossipKey,
		oidcURL:          oidcURL,
	}, inputs.Password)
	if err != nil {
		return err
	}

	err = os.WriteFile(s.ConsulHome+"/password", []byte(s.Password), 0400)
	if err != nil {
		return err
	}

	err = command.UploadCredentialsToLDAP(ldapHandler, s.ClusterCredential)
	if err != nil {
		return errors.WithMessage(err, "unable to upload credentials file to LDAP")
	}

	if !isContainer {
		err = systemd.EnableSystemdUnit(deps.SystemdUnitHandler, serviceDiscoverUnit)
		if err != nil {
			return errors.Errorf("unable to enable %s unit: %s", serviceDiscoverUnit, err)
		}
	}

	return nil
}

// bootstrapACL bootstraps the Consul ACL system and creates the token of this server. It returns the raw bootstrap
// response, to be shared with the other servers of the cluster, and the secret of the bootstrap (root) token.
func (s *Setup) bootstrapACL(deps businessDependencies, hostname string) ([]byte, string, error) {
	aclBootstrapJSON, err := s.createACLBootstrapToken(deps)
	if err != nil {
		return nil, "", err
	}

	aclBootstrap := &command.ACLTokenCreation{}

	err = json.Unmarshal(aclBootstrapJSON, aclBootstrap)
	if err != nil {
		return nil, "", errors.WithMessage(
			err,
			"unable to decode ACL bootstrap response from Consul",
		)
	}

	serverToken, err := command.CreateACLToken(deps.CreateCommand, command.Server, hostname, aclBootstrap.SecretID)
	if err != nil {
		return nil, "", err
	}

	err = command.SetACLToken(deps.CreateCommand, serverToken, aclBootstrap.SecretID)
	if err != nil {
		return nil, "", err
	}

	return aclBootstrapJSON, aclBootstrap.SecretID, nil
}

// clusterSecrets is what the first server shares with the other servers of the cluster through the cluster credentials.
type clusterSecrets struct {
	aclBootstrapJSON []byte
	gossipKey        string
	oidcURL          string
}

// createClusterCredentials stores the cluster secrets, together with the CA of the cluster, in the PGP encrypted
// cluster credentials archive.
func (s *Setup) createClusterCredentials(secrets clusterSecrets, password string) error {
	contents := map[string][]byte{
		spire.ClusterOIDCURLFile:   []byte(secrets.oidcURL),
		command.GossipKey:          []byte(secrets.gossipKey),
		command.ConsulACLBootstrap: secrets.aclBootstrapJSON,
	}

	filesToCompress := map[string]string{
		s.ConsulHome + "/" + command.ConsulCA:    s.ConsulHome + "/" + command.ConsulCA,
		s.ConsulHome + "/" + command.ConsulCAKey: s.ConsulHome + "/" + command.ConsulCAKey,
	}

	defer func() {
		for name, path := range filesToCompress {
			if _, secret := contents[name]; secret {
				_ = os.Remove(path)
			}
		}
	}()

	for name, content := range contents {
		path, err := writeTempFile(name, content)
		if err != nil {
			return err
		}

		filesToCompress[name] = path
	}

	return s.createEncryptedSecret(filesToCompress, password)
}

// writeTempFile writes the content in a new temporary file and returns its path. The caller must remove the file.
func writeTempFile(pattern string, content []byte) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}

	_ = file.Close()

	err = os.WriteFile(file.Name(), content, 0600)
	if err != nil {
		_ = os.Remove(file.Name())

		return "", err
	}

	return file.Name(), nil
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

// startServiceDiscover starts the service-discover server, either as a container daemon or as a systemd unit, then
// waits for Consul to elect a Raft leader. It reports whether the setup is running inside a container.
func startServiceDiscover(deps businessDependencies) (bool, error) {
	isContainer, err := startServiceDiscoverDaemon(deps)
	if err != nil {
		return isContainer, err
	}

	return isContainer, waitForConsulLeader(deps)
}

// startServiceDiscoverDaemon starts the service-discover server, either as a container daemon or as a systemd unit.
// It reports whether the setup is running inside a container.
func startServiceDiscoverDaemon(deps businessDependencies) (bool, error) {
	isContainer := command.CheckDockerContainer()

	if isContainer && !testingMode {
		cmd := exec.Command("service-discoverd-docker", "server") //nolint:noctx // no context available here

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

	return isContainer, nil
}

// waitForConsulLeader blocks until the local Consul agent reports a Raft leader. systemd considers the
// service-discover unit ready as soon as Consul joins the LAN gossip pool, which happens before the leader election:
// running the ACL bootstrap before a leader exists makes it fail with "No cluster leader".
func waitForConsulLeader(deps businessDependencies) error {
	deadline := time.Now().Add(consulLeaderTimeout)

	for {
		leader, err := deps.ConsulLeader()
		if err == nil && leader != "" {
			return nil
		}

		if time.Now().After(deadline) {
			if err != nil {
				return errors.WithMessage(err, "timeout reached while waiting for consul leader election")
			}

			return errors.New("timeout reached while waiting for consul leader election")
		}

		time.Sleep(consulLeaderPollInterval)
	}
}

// noClusterLeaderErr is the error Consul returns for requests that need a Raft leader while none is elected yet.
const noClusterLeaderErr = "No cluster leader"

func (s *Setup) createACLBootstrapToken(deps businessDependencies) ([]byte, error) {
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
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					// Consul keeps answering "No cluster leader" until the Raft leader election completes: keep
					// polling in that case, any other error is fatal.
					stderr := strings.TrimSpace(string(ee.Stderr))
					if !strings.Contains(stderr, noClusterLeaderErr) {
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

// setupSpireIntegration completes the SPIRE integration once Consul is running and its ACL system bootstrapped:
// the "spire" auth method lets the services log in with their JWT-SVID, and the SPIRE agent of this server gets
// attested so that the services it hosts have an identity.
func (s *Setup) setupSpireIntegration(
	deps businessDependencies, hostname, bindAddress, oidcURL, rootToken string,
) error {
	caPEM, err := os.ReadFile(filepath.Join(s.ConsulHome, command.ConsulCA)) // #nosec
	if err != nil {
		return errors.WithMessage(err, "unable to read the Consul CA")
	}

	err = spire.EnsureAuthMethod(deps.CreateCommand, rootToken, oidcURL, string(caPEM))
	if err != nil {
		return err
	}

	if !manageSpireUnits() {
		return nil
	}

	err = s.setupSpireAgent(deps, hostname, bindAddress)
	if err != nil {
		return errors.WithMessage(err, "unable to set up the SPIRE agent of this server")
	}

	return nil
}

func (s *Setup) generateCertificationAuthority(deps businessDependencies, hostname string) error {
	certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)

	err := exec2.InPath(
		deps.CreateCommand(consulBin,
			"tls",
			"ca",
			"create",
			certificateDaysFlag,
			"-name-constraint",
			// The OIDC discovery provider of SPIRE is served with a certificate for the server hostname signed by this CA.
			"-additional-name-constraint="+hostname),
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
