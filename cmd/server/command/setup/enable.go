// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/spire"
	"github.com/zextras/service-discover/pkg/term"
)

// Spire groups the commands managing SPIRE on a server.
type Spire struct {
	Finalize Finalize `cmd:"" help:"Revoke the legacy ACL token and policy of an agent migrated to SPIRE with setup --join --migrate, and the legacy tokens of its services that logged in with SPIRE, after checking that it rejoined with auto_config. Run it with the cluster credentials, on a server"` //nolint:lll
	Enable   Enable   `cmd:"" help:"Set up SPIRE on a Consul server that was installed without it, so that agents can join with a SPIRE join bundle. Run it once on every server, one at a time"`                                                                                                           //nolint:lll
}

// NewSpire returns the spire command with the real paths of the installation.
func NewSpire() Spire {
	return Spire{Enable: Enable{setup: NewSetup()}, Finalize: Finalize{setup: NewSetup()}}
}

// Enable brings SPIRE to an existing Consul server. It is idempotent.
type Enable struct {
	setup Setup `kong:"-"`

	Password           string `optional:"" help:"Password of the cluster credentials. If none is set, it is asked"`
	SpireDatastoreFile string `optional:"" help:"File (or - for stdin) with the PostgreSQL connection string of the datastore shared by the SPIRE servers of all the Consul servers (high availability). Without it a single SPIRE server with a local SQLite database is set up on the first server enabled"` //nolint:lll
}

type enableOutput struct {
	Steps []string `json:"steps"`
}

func (o *enableOutput) PlainRender() (string, error) {
	return strings.Join(o.Steps, "\n") + "\n", nil
}

func (o *enableOutput) JSONRender() (string, error) {
	return formatter.DefaultJSONRender(o)
}

func (o *enableOutput) add(step string) {
	o.Steps = append(o.Steps, step)
}

// clusterSecrets are the secrets of the cluster needed to enable SPIRE.
type clusterSecrets struct {
	password string
	token    string
	caKeyPEM []byte
}

func (e *Enable) Run(commonFlags *command.GlobalCommonFlags) error {
	var err error

	e.setup.spireDSN, err = spire.ReadDatastoreDSN(e.SpireDatastoreFile, os.Stdin)
	if err != nil {
		return err
	}

	password := e.Password

	if password == "" {
		password, err = e.askPassword()
		if err != nil {
			return err
		}
	}

	deps := realDependencies{}

	out, err := e.setup.enableSpire(deps, func() (clusterSecrets, error) {
		return e.setup.readClusterSecrets(password)
	})
	if err != nil {
		return err
	}

	render, err := formatter.Render(out, commonFlags.Format)
	if err != nil {
		return err
	}

	_, err = os.Stdout.WriteString(render)

	return err
}

func (e *Enable) askPassword() (string, error) {
	if e.SpireDatastoreFile == "-" {
		return "", errors.New("stdin carries the datastore connection string: give the password with --password")
	}

	return askClusterPassword()
}

func askClusterPassword() (string, error) {
	terminal, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return "", err
	}

	defer terminal.Close()

	password, err := terminal.ReadPassword("Insert the cluster credential password: ")
	if err != nil {
		var notATerminal term.NotATerminalError
		if !errors.As(err, &notATerminal) {
			return "", err
		}

		return term.MustRead(terminal.ReadLine()), nil
	}

	return password, nil
}

// readClusterSecrets reads the bootstrap token, the CA and its key from the cluster credentials of this server. The
// token in the environment, when given, wins.
func (s *Setup) readClusterSecrets(password string) (clusterSecrets, error) {
	clusterCredential, err := command.OpenClusterCredential(s.ClusterCredential)
	if err != nil {
		return clusterSecrets{}, err
	}

	defer clusterCredential.Close()

	credReader, err := encrypter.NewReader(clusterCredential, []byte(password))
	if err != nil {
		return clusterSecrets{}, errors.Errorf("unable to open %s: %s", clusterCredential.Name(), err)
	}

	caPath, err := filepath.Rel("/", filepath.Join(config.ConsulHome, command.ConsulCA))
	if err != nil {
		return clusterSecrets{}, err
	}

	caKeyPath, err := filepath.Rel("/", filepath.Join(config.ConsulHome, command.ConsulCAKey))
	if err != nil {
		return clusterSecrets{}, err
	}

	files, err := encrypter.ReadFiles(credReader, caPath, caKeyPath, command.ConsulACLBootstrap)
	if err != nil {
		return clusterSecrets{}, err
	}

	bootstrap := command.ACLTokenCreation{}

	err = json.Unmarshal(files[command.ConsulACLBootstrap], &bootstrap)
	if err != nil {
		return clusterSecrets{}, errors.WithMessage(err, "unable to decode ACL Bootstrap token")
	}

	secrets := clusterSecrets{password: password, token: bootstrap.SecretID, caKeyPEM: files[caKeyPath]}

	if token := os.Getenv(command.SetupConsulToken); token != "" {
		secrets.token = token
	}

	return secrets, nil
}

// enableSpire sets up SPIRE on this Consul server: its SPIRE server and OIDC discovery provider (when this server has
// to run them), the local SPIRE agent, the auto_config authorization of Consul and the "spire" auth method. It can be
// run again at any time: what is already done is left alone. The Consul agent of the server is restarted when its
// configuration changes, so this has to be run on one server at a time and only while the Raft cluster has a leader.
//
//nolint:cyclop,funlen // sequential flow
func (s *Setup) enableSpire(
	deps businessDependencies, loadSecrets func() (clusterSecrets, error),
) (formatter.Formatter, error) {
	out := &enableOutput{}

	err := s.enablePreflight(deps)
	if err != nil {
		return nil, err
	}

	consulConfig, err := readJSONObject(s.ConsulFileConfig)
	if err != nil {
		return nil, err
	}

	if server, _ := consulConfig["server"].(bool); !server {
		return nil, errors.New("this is not a Consul server: SPIRE is enabled on the servers only")
	}

	bindAddress, err := s.readBindAddress()
	if err != nil {
		return nil, err
	}

	localConfig, err := carbonio.LoadLocalConfig(s.LocalConfigPath)
	if err != nil {
		return nil, err
	}

	hostname, err := command.RetrieveZimbraHostname(localConfig, deps.LdapHandler(localConfig))
	if err != nil {
		return nil, err
	}

	leader, err := deps.ConsulLeader()
	if err != nil || leader == "" {
		return nil, errors.New("the Raft cluster has no leader: fix it before touching the configuration of a server")
	}

	secrets, err := loadSecrets()
	if err != nil {
		return nil, err
	}

	jwksURL, runSpire, err := s.spireMode(deps, secrets.token, hostname, bindAddress)
	if err != nil {
		return nil, err
	}

	if runSpire {
		served, err := s.enableSpireServer(deps, secrets, hostname, bindAddress, out)
		if err != nil {
			return nil, err
		}

		if jwksURL == "" {
			jwksURL = served
		}
	}

	changed, err := s.enableAutoConfig(deps, consulConfig, jwksURL)
	if err != nil {
		return nil, err
	}

	if changed {
		out.add("Consul configuration updated with the SPIRE authorization of the agents, restarting " +
			serviceDiscoverUnit)

		err = restartServiceDiscover(deps)
		if err != nil {
			return nil, err
		}
	}

	err = s.enableAuthMethod(deps, secrets.token, jwksURL, leader, bindAddress, out)
	if err != nil {
		return nil, err
	}

	published, err := s.publishJWKSURL(deps.LdapHandler(localConfig), secrets.password, jwksURL)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to add the SPIRE settings to the cluster credentials")
	}

	if published {
		out.add("Cluster credentials updated (and uploaded to LDAP): the servers added later will get SPIRE too")
	}

	out.add("SPIRE is enabled on this server")

	return out, nil
}

func (s *Setup) enablePreflight(deps businessDependencies) error {
	err := deps.CreateCommand(consulBin, "version").Run()
	if err != nil {
		return errors.Errorf("unable to execute consul binary: %s", err)
	}

	if deps.GetuidSyscall() != rootUID {
		return errors.New("this command must be executed as root")
	}

	if command.CheckDockerContainer() && !testingMode {
		return errors.New("SPIRE cannot be enabled inside a container")
	}

	_, err = os.Stat(s.ConsulFileConfig)
	if err != nil {
		return errors.New("service-discover is not set up on this server")
	}

	return nil
}

func (s *Setup) readBindAddress() (string, error) {
	content, err := os.ReadFile(s.MutableConfigFile) // #nosec
	if err != nil {
		return "", errors.WithMessage(err, "unable to read the binding address of this server")
	}

	mutable := command.MutableConsulConfig{}

	err = json.Unmarshal(content, &mutable)
	if err != nil || mutable.BindAddress == "" {
		return "", errors.Errorf("unable to read the binding address from %s", s.MutableConfigFile)
	}

	return mutable.BindAddress, nil
}

// spireMode tells how the cluster uses SPIRE, from the "spire" auth method that is shared by all the servers: where
// the Consul servers fetch the keys from, and whether this server has to run its own SPIRE server.
//   - the keys are on the loopback address: high availability, every server runs SPIRE on the shared datastore;
//   - the keys are on another address: a single SPIRE server, that this server runs only if the keys are its own;
//   - no auth method yet: this is the first server enabled, SPIRE is run here.
func (s *Setup) spireMode(deps businessDependencies, token, hostname, bindAddress string) (string, bool, error) {
	current, exists, err := spire.AuthMethodJWKSURL(deps.CreateCommand, token)
	if err != nil {
		return "", false, err
	}

	switch {
	case exists && current == spire.LocalJWKSURL:
		if !s.highAvailability() {
			return "", false, errors.New("the SPIRE servers of this cluster share a PostgreSQL datastore: " +
				"give its connection string with --spire-datastore-file")
		}

		return current, true, nil
	case exists:
		if s.highAvailability() {
			return "", false, errors.New("this cluster runs a single SPIRE server: --spire-datastore-file cannot be used")
		}

		return current, current == spire.JWKSURL(hostname) || current == spire.JWKSURL(bindIPString(bindAddress)), nil
	case s.highAvailability():
		return spire.LocalJWKSURL, true, nil
	default:
		return "", true, nil // the URL is known once the OIDC provider of this server is set up
	}
}

func (s *Setup) enableSpireServer(
	deps businessDependencies, secrets clusterSecrets, hostname, bindAddress string, out *enableOutput,
) (string, error) {
	caKeyPath := filepath.Join(s.ConsulHome, command.ConsulCAKey)

	_, err := os.Stat(caKeyPath)
	if err != nil {
		if len(secrets.caKeyPEM) == 0 {
			return "", errors.New("unable to find the Consul CA key")
		}

		err = os.WriteFile(caKeyPath, secrets.caKeyPEM, 0600)
		if err != nil {
			return "", err
		}

		defer os.Remove(caKeyPath)
	}

	jwksURL, err := s.setupSpireServer(deps, hostname, bindAddress, true)
	if err != nil {
		return "", errors.WithMessage(err, "unable to set up the SPIRE server")
	}

	out.add("SPIRE server and OIDC discovery provider running")

	if !manageSpireUnits() {
		return jwksURL, nil
	}

	_, err = os.Stat(filepath.Join(s.SpireConfigDir, spire.AgentConfFile))
	if err == nil {
		return jwksURL, nil // the SPIRE agent of this server is already attested
	}

	err = s.setupSpireAgent(deps, hostname, bindAddress)
	if err != nil {
		return "", errors.WithMessage(err, "unable to set up the SPIRE agent of this server")
	}

	out.add("SPIRE agent of this server running")

	return jwksURL, nil
}

// enableAutoConfig adds the auto_config authorization to the Consul configuration of this server, keeping the rest
// as it is. It tells whether the file changed.
func (s *Setup) enableAutoConfig(deps businessDependencies, consulConfig map[string]any, jwksURL string) (bool, error) {
	caPEM, err := os.ReadFile(filepath.Join(s.ConsulHome, command.ConsulCA)) // #nosec
	if err != nil {
		return false, errors.WithMessage(err, "unable to read the Consul CA")
	}

	wanted, err := toGeneric(newAutoConfig(jwksURL, string(caPEM)))
	if err != nil {
		return false, err
	}

	if reflect.DeepEqual(consulConfig["auto_config"], wanted) {
		return false, nil
	}

	consulConfig["auto_config"] = wanted

	content, err := json.MarshalIndent(consulConfig, "", "  ")
	if err != nil {
		return false, err
	}

	err = os.WriteFile(s.ConsulFileConfig, content, os.FileMode(0600))
	if err != nil {
		return false, errors.Errorf("unable to save the configuration file %s: %s", s.ConsulFileConfig, err)
	}

	return true, permissions.SetStrictPermissions(deps, s.ConsulFileConfig)
}

// enableAuthMethod makes sure the "spire" auth method points to jwksURL. Consul fetches the keys while saving the
// method, from the Raft leader: with a loopback URL (high availability) that is the leader that has to do it, so the
// other servers leave the method to it.
func (s *Setup) enableAuthMethod(
	deps businessDependencies, token, jwksURL, leader, bindAddress string, out *enableOutput,
) error {
	current, exists, err := spire.AuthMethodJWKSURL(deps.CreateCommand, token)
	if err != nil {
		return err
	}

	if exists && current == jwksURL {
		return nil
	}

	if jwksURL == spire.LocalJWKSURL && !isLeader(leader, bindAddress) {
		out.add("The auth method \"" + spire.AuthMethodName + "\" is not created: only the Raft leader can " +
			"do it when every server runs SPIRE. Run this command again on the leader once every server is enabled")

		return nil
	}

	caPEM, err := os.ReadFile(filepath.Join(s.ConsulHome, command.ConsulCA)) // #nosec
	if err != nil {
		return errors.WithMessage(err, "unable to read the Consul CA")
	}

	err = spire.EnsureAuthMethod(deps.CreateCommand, token, jwksURL, string(caPEM))
	if err != nil {
		return err
	}

	out.add("Auth method \"" + spire.AuthMethodName + "\" configured")

	return nil
}

func bindIPString(bindAddress string) string {
	return strings.Split(bindAddress, "/")[0]
}

func isLeader(leader, bindAddress string) bool {
	host, _, err := net.SplitHostPort(leader)
	if err != nil {
		host = leader
	}

	return host == bindIPString(bindAddress)
}

// restartServiceDiscover restarts the Consul agent and waits for it to see a Raft leader again: the next server
// must not be touched before the cluster is healthy.
func restartServiceDiscover(deps businessDependencies) error {
	_, err := deps.CreateCommand("systemctl", "restart", serviceDiscoverUnit).Output()
	if err != nil {
		return exec.ErrorFromStderr(err, "unable to restart "+serviceDiscoverUnit)
	}

	return waitForConsulLeader(deps)
}

func readJSONObject(path string) (map[string]any, error) {
	content, err := os.ReadFile(path) // #nosec
	if err != nil {
		return nil, errors.WithMessagef(err, "unable to read %s", path)
	}

	object := map[string]any{}

	err = json.Unmarshal(content, &object)
	if err != nil {
		return nil, errors.WithMessagef(err, "unable to decode %s", path)
	}

	return object, nil
}

func toGeneric(value any) (any, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	var generic any

	decoder := json.NewDecoder(bytes.NewReader(content))

	err = decoder.Decode(&generic)

	return generic, err
}
