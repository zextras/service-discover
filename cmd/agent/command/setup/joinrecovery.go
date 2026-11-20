// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/spire"
)

// joinPendingFile is created in the Consul home when a join starts and removed when it succeeds. Finding it means
// that a previous attempt did not complete, and left a partial SPIRE agent and Consul client behind.
const joinPendingFile = "join-in-progress"

func (s *Setup) joinPendingPath() string {
	return filepath.Join(s.ConsulHome, joinPendingFile)
}

// joinPending tells whether a previous --join did not complete and can be cleaned up and redone. The Consul
// configuration of a setup done with the cluster credentials is never a leftover of a join: if the marker is there
// next to one, it is stale and ignored.
func (s *Setup) joinPending() bool {
	_, err := os.Stat(s.joinPendingPath())
	if err != nil {
		return false
	}

	content, err := os.ReadFile(s.ConsulFileConfig) // #nosec
	if err != nil {
		return os.IsNotExist(err)
	}

	config := struct {
		AutoConfig struct {
			Enabled bool `json:"enabled"`
		} `json:"auto_config"`
	}{}

	return json.Unmarshal(content, &config) == nil && config.AutoConfig.Enabled
}

// cleanupJoin removes what a join writes: the SPIRE agent with its state, the Consul client with its configuration
// and data, and the name of the SPIRE servers. Everything is attempted, the first error is returned. The units are
// stopped first, so that nothing keeps using what is deleted.
func (s *Setup) cleanupJoin(deps businessDependencies) error {
	for _, unit := range []string{serviceDiscoverUnit, spire.AgentUnit} {
		stopUnit(deps, unit)
	}

	var first error

	keep := func(err error) {
		if err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}

	for _, path := range []string{
		s.ConsulFileConfig,
		filepath.Join(s.ConsulHome, introTokenFile),
		filepath.Join(s.ConsulHome, command.ConsulCA),
		filepath.Join(s.SpireConfigDir, spire.AgentConfFile),
		filepath.Join(s.SpireConfigDir, spire.BundleFile),
	} {
		keep(os.Remove(path))
	}

	keep(emptyDir(s.ConsulData))
	keep(emptyDir(s.SpireDataDir))
	keep(removeHostsBlock(s.HostsFile))

	if first == nil {
		keep(os.Remove(s.joinPendingPath()))
	}

	return first
}

// emptyDir deletes what a directory holds, but not the directory itself: it is managed by systemd.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	for _, entry := range entries {
		err = os.RemoveAll(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
	}

	return nil
}

// stopUnit stops a unit, waiting for it. A unit that does not exist or is not running is not an error here.
func stopUnit(deps businessDependencies, unit string) {
	conn, err := deps.SystemdUnitHandler()
	if err != nil {
		return
	}

	defer conn.Close()

	result := make(chan string, 1)

	_, err = conn.StopUnit(unit, "replace", result)
	if err == nil {
		<-result
	}
}
