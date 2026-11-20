// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/spire"
	"github.com/zextras/service-discover/pkg/systemd"
)

const (
	// migrateMarkerFile exists, with the backup directory complete, from the moment the configuration of a
	// migration is touched until it succeeds or is rolled back.
	migrateMarkerFile = "migrate-in-progress"
	migrateBackupDir  = "migrate-backup"
	// aclTokensFile and autoConfigFile are the files of the data directory where Consul keeps the tokens set at
	// runtime and the answer of the servers to the auto_config request.
	aclTokensFile  = "acl-tokens.json" //nolint:gosec // a file name
	autoConfigFile = "auto-config.json"
)

// migrateRejoinTimeout is how long the agent has to rejoin the cluster after the switch before the previous
// configuration is restored.
var (
	migrateRejoinTimeout = 2 * time.Minute
	migratePollInterval  = 2 * time.Second
)

type migrateOutput struct {
	Steps []string `json:"steps"`
}

func (o *migrateOutput) PlainRender() (string, error) { return strings.Join(o.Steps, "\n") + "\n", nil }

func (o *migrateOutput) JSONRender() (string, error) { return formatter.DefaultJSONRender(o) }

func (s *Setup) runMigrate(commonFlags *command.GlobalCommonFlags, deps businessDependencies, writer io.Writer) error {
	err := deps.CreateCommand(command.ConsulBin, "version").Run()
	if err != nil {
		return errors.Errorf("unable to execute consul binary: %s", err)
	}

	if deps.GetuidSyscall() != rootUID {
		return errors.New("this command must be executed as root")
	}

	out, err := s.migrate(deps)
	if err != nil {
		return err
	}

	render, err := formatter.Render(out, commonFlags.Format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(writer, render)

	return err
}

type consulConfigKind int

const (
	configMissing consulConfigKind = iota
	configTarball
	configJoined
	configServer
)

// existingConfig tells which kind of setup the Consul configuration of this host belongs to, and its node name.
func (s *Setup) existingConfig() (consulConfigKind, string, error) {
	content, err := os.ReadFile(s.ConsulFileConfig) // #nosec
	if err != nil {
		if os.IsNotExist(err) {
			return configMissing, "", nil
		}

		return configMissing, "", err
	}

	config := struct {
		NodeName   string `json:"node_name"`
		Server     bool   `json:"server"`
		AutoConfig struct {
			Enabled bool `json:"enabled"`
		} `json:"auto_config"`
	}{}

	err = json.Unmarshal(content, &config)
	if err != nil {
		return configMissing, "", errors.WithMessagef(err, "unable to decode %s", s.ConsulFileConfig)
	}

	switch {
	case config.Server:
		return configServer, config.NodeName, nil
	case config.AutoConfig.Enabled:
		return configJoined, config.NodeName, nil
	default:
		return configTarball, config.NodeName, nil
	}
}

func (s *Setup) migrateBackupPath() string { return filepath.Join(s.ConsulHome, migrateBackupDir) }

func (s *Setup) migrateMarkerPath() string { return filepath.Join(s.ConsulHome, migrateMarkerFile) }

// migrate turns an agent set up with the cluster credentials into one joined through SPIRE. The old configuration is
// backed up before anything is touched; when the agent does not rejoin the cluster, or any step fails, it is put
// back and restarted, so that the node is left as it was. Once it worked, the secrets the old setup left on the host
// are removed. Running it on a node already migrated only removes what may be left. A migration interrupted by a
// crash is rolled back by the next run, before it starts again.
//
// The ACL token the old setup created for the agent is removed from this host but stays valid in Consul: the policy
// of that token cannot delete tokens, so only a server can revoke it. The output says so.
//
//nolint:cyclop // sequential flow
func (s *Setup) migrate(deps businessDependencies) (formatter.Formatter, error) {
	out := &migrateOutput{}

	_, err := os.Stat(s.migrateMarkerPath())
	if err == nil {
		err = s.rollbackMigration(deps)
		if err != nil {
			return nil, errors.WithMessage(err, "unable to restore the configuration saved by an interrupted migration")
		}

		out.add("The configuration saved by an interrupted migration was restored")
	}

	_ = os.RemoveAll(s.migrateBackupPath()) // a backup without marker was never complete: nothing was changed

	kind, nodeName, err := s.existingConfig()
	if err != nil {
		return nil, err
	}

	switch kind {
	case configMissing:
		return nil, errors.New("service-discover is not set up on this node: use --join without --migrate")
	case configServer:
		return nil, errors.New("this node is a Consul server: use 'service-discover spire enable'")
	case configJoined:
		err = s.removeOldSetup()
		if err != nil {
			return nil, err
		}

		out.add("This node already joined through SPIRE: nothing to migrate")

		return out, nil
	case configTarball:
	}

	bundle, err := s.readJoinBundle()
	if err != nil {
		return nil, err
	}

	if bundle.NodeName != nodeName {
		return nil, errors.Errorf("the join bundle is for the node %q but this one is %q: generate it for this node",
			bundle.NodeName, nodeName)
	}

	err = s.checkMigrateBindAddress(deps)
	if err != nil {
		return nil, err
	}

	if command.CheckDockerContainer() && !testingMode {
		return nil, errors.New("joining through SPIRE is not supported inside a container")
	}

	err = s.backupForMigration()
	if err != nil {
		return nil, errors.WithMessage(err, "unable to back up the current configuration")
	}

	err = s.switchToSpire(deps, bundle)
	if err != nil {
		rollback := s.rollbackMigration(deps)
		if rollback != nil {
			return nil, errors.Errorf("%s. The previous configuration could not be restored: %s. It is saved in %s",
				err, rollback, s.migrateBackupPath())
		}

		return nil, errors.WithMessage(err, "the migration failed and the previous configuration was restored")
	}

	err = s.removeOldSetup()
	if err != nil {
		return nil, err
	}

	_ = os.RemoveAll(s.migrateBackupPath())

	err = os.Remove(s.migrateMarkerPath())
	if err != nil {
		return nil, err
	}

	out.add("Node " + nodeName + " migrated to SPIRE: the agent joined the cluster with auto_config")
	out.add("Removed the cluster credentials copy, the agent certificate and the CA key remnants")
	out.add("The old ACL token of this agent was removed from this host, but it is still valid in Consul: " +
		"revoke it from a Consul server, as root with the cluster credentials password:\n    " +
		"service-discover spire finalize --host " + s.hostnameForFinalize())

	return out, nil
}

// hostnameForFinalize is the host to give to 'spire finalize': the hostname of the local configuration, as the node
// name replaces its dots.
func (s *Setup) hostnameForFinalize() string {
	localConfig, err := carbonio.LoadLocalConfig(s.LocalConfigPath)
	if err == nil {
		hostname := localConfig.Value(carbonio.LocalConfigServerHostname)
		if hostname != "" {
			return hostname
		}
	}

	return "<hostname of this node>"
}

func (o *migrateOutput) add(step string) { o.Steps = append(o.Steps, step) }

func (s *Setup) checkMigrateBindAddress(deps businessDependencies) error {
	bind := s.BindAddress

	if bind == "" {
		content, err := os.ReadFile(s.MutableConfigFile) // #nosec
		if err != nil {
			return errors.WithMessage(err, "unable to read the binding address of this node: give it as argument")
		}

		mutable := command.MutableConsulConfig{}

		err = json.Unmarshal(content, &mutable)
		if err != nil || mutable.BindAddress == "" {
			return errors.New("unable to read the binding address of this node: give it as argument")
		}

		bind = mutable.BindAddress
		s.BindAddress = bind
	}

	networks, err := command.NonLoopbackInterfaces(deps)
	if err != nil {
		return err
	}

	return command.CheckValidBindingAddress(deps, networks, bind)
}

// backupForMigration saves what the rollback has to restore, and only then writes the marker.
func (s *Setup) backupForMigration() error {
	err := os.MkdirAll(s.migrateBackupPath(), 0700)
	if err != nil {
		return err
	}

	for source, name := range map[string]string{
		s.ConsulFileConfig:                            filepath.Base(s.ConsulFileConfig),
		filepath.Join(s.ConsulData, aclTokensFile):    aclTokensFile,
		filepath.Join(s.ConsulHome, command.ConsulCA): command.ConsulCA,
	} {
		content, err := os.ReadFile(source) // #nosec
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return err
		}

		err = os.WriteFile(filepath.Join(s.migrateBackupPath(), name), content, 0600) //nolint:gosec // configured paths
		if err != nil {
			return err
		}
	}

	return os.WriteFile(s.migrateMarkerPath(), nil, 0600)
}

// switchToSpire attests the SPIRE agent, replaces the configuration of the Consul agent and restarts it, then waits
// for it to rejoin the cluster.
func (s *Setup) switchToSpire(deps businessDependencies, bundle *spire.JoinBundle) error {
	spireHost, spirePort, err := splitServerAddress(bundle.SpireServer)
	if err != nil {
		return err
	}

	err = s.writeStrict(deps, filepath.Join(s.ConsulHome, command.ConsulCA), []byte(bundle.ConsulCA))
	if err != nil {
		return err
	}

	serverAddress, err := s.spireServerAddress(deps, spireHost, spirePort, bundle.ConsulServers)
	if err != nil {
		return err
	}

	token, err := s.attestAndFetchToken(deps, bundle, serverAddress, spirePort)
	if err != nil {
		return err
	}

	err = s.writeStrict(deps, filepath.Join(s.ConsulHome, introTokenFile), []byte(token))
	if err != nil {
		return err
	}

	// the old token and the answer of the servers to a previous auto_config must not survive the switch
	for _, name := range []string{aclTokensFile, autoConfigFile} {
		err = os.Remove(filepath.Join(s.ConsulData, name))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	err = s.writeJoinConfig(deps, bundle)
	if err != nil {
		return err
	}

	err = s.restartConsul(deps)
	if err != nil {
		return err
	}

	return waitForRejoin()
}

func (s *Setup) restartConsul(deps businessDependencies) error {
	stopUnit(deps, serviceDiscoverUnit)

	err := systemd.StartSystemdUnit(deps.SystemdUnitHandler, serviceDiscoverUnit)
	if err != nil {
		return errors.WithMessagef(err, "unable to start %s", serviceDiscoverUnit)
	}

	return nil
}

// waitForRejoin waits until the Consul agent sees the Raft leader, that is until it joined the servers again.
func waitForRejoin() error {
	deadline := time.Now().Add(migrateRejoinTimeout)

	var lastErr error

	for {
		leader, err := consulStatusLeader()
		if err == nil && leader != "" {
			return nil
		}

		lastErr = err

		if time.Now().After(deadline) {
			break
		}

		time.Sleep(migratePollInterval)
	}

	return errors.Errorf("the agent did not rejoin the cluster within %s (%v)", migrateRejoinTimeout, lastErr)
}

// rollbackMigration puts the saved configuration back, removes what the switch wrote and restarts Consul with the
// old setup.
func (s *Setup) rollbackMigration(deps businessDependencies) error {
	stopUnit(deps, serviceDiscoverUnit)

	err := s.removeSpireAgent(deps)
	if err != nil {
		return err
	}

	err = os.Remove(filepath.Join(s.ConsulData, autoConfigFile))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	err = os.Remove(filepath.Join(s.ConsulData, aclTokensFile))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	for name, destination := range map[string]string{
		filepath.Base(s.ConsulFileConfig): s.ConsulFileConfig,
		aclTokensFile:                     filepath.Join(s.ConsulData, aclTokensFile),
		command.ConsulCA:                  filepath.Join(s.ConsulHome, command.ConsulCA),
	} {
		content, err := os.ReadFile(filepath.Join(s.migrateBackupPath(), name)) // #nosec
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return err
		}

		err = s.writeStrict(deps, destination, content)
		if err != nil {
			return err
		}
	}

	err = s.restartConsul(deps)
	if err != nil {
		return err
	}

	_ = os.RemoveAll(s.migrateBackupPath())

	return os.Remove(s.migrateMarkerPath())
}

// removeOldSetup deletes what the setup with the cluster credentials left on the host and the join does not use: the
// copy of the cluster credentials (it holds the CA key and the bootstrap ACL token, encrypted), any CA key remnant
// and the certificate of the agent, valid for decades. The CA certificate stays: it is the trust anchor.
func (s *Setup) removeOldSetup() error {
	for _, path := range []string{
		s.ClusterCredential,
		filepath.Join(s.ConsulHome, command.ConsulCAKey),
		filepath.Join(s.ConsulHome, command.ConsulAgentCertificate),
		filepath.Join(s.ConsulHome, command.ConsulAgentCertificateKey),
	} {
		err := os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return errors.WithMessagef(err, "unable to remove %s", path)
		}
	}

	return nil
}
