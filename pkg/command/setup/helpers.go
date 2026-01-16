// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"context"
	"encoding/json"
	"os"
	stdexec "os/exec"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/permissions"
	"github.com/zextras/service-discover/pkg/systemd"
)

const (
	// ServiceDiscoverUnit is the systemd unit name for service-discover.
	ServiceDiscoverUnit = "service-discover.service"
)

// TestingMode can be set to true during tests to skip Docker container operations.
var TestingMode bool

// StartServiceDiscoverMode starts service-discover in the specified mode (agent or server).
// It handles both container and systemd environments.
func StartServiceDiscoverMode(deps BusinessDependencies, mode string) (bool, error) {
	isContainer := command.CheckDockerContainer()

	if isContainer && !TestingMode {
		// #nosec G204 -- mode is always a hardcoded constant ("agent" or "server"), not user input
		cmd := stdexec.CommandContext(context.Background(), "service-discoverd-docker", mode)

		err := cmd.Run()
		if err != nil {
			return isContainer, errors.WithMessagef(err, "unable to start service-discoverd %s", mode)
		}
	} else {
		err := systemd.StartSystemdUnit(deps.SystemdUnitHandler, ServiceDiscoverUnit)
		if err != nil {
			return isContainer, errors.WithMessagef(err, "unable to start %s", ServiceDiscoverUnit)
		}
	}

	return isContainer, nil
}

// SaveBindAddressWithPermissions saves the bind address configuration and sets strict permissions.
// This combines two operations that always appear together in the setup workflows.
func SaveBindAddressWithPermissions(deps BusinessDependencies, configPath, bindAddress string) error {
	err := command.SaveBindAddressConfiguration(configPath, bindAddress)
	if err != nil {
		return err
	}

	return permissions.SetStrictPermissions(deps, configPath)
}

// WriteFileWithStrictPermissions writes content to a file and sets strict permissions.
// This helper reduces duplication across setup files where we repeatedly write files and
// immediately set permissions.
func WriteFileWithStrictPermissions(deps BusinessDependencies, path string, data []byte, perm os.FileMode) error {
	err := os.WriteFile(path, data, perm)
	if err != nil {
		return err
	}

	return permissions.SetStrictPermissions(deps, path)
}

// ACLTokenFromExtractedFiles sets up ACL tokens from the extracted credential files.
// This is the common pattern used across agent setup, server firstsetup, and server importsetup.
func ACLTokenFromExtractedFiles(
	deps BusinessDependencies,
	role command.ConsulRole,
	zimbraHostname string,
	extractedFiles map[string][]byte,
) error {
	aclBootstrapToken := command.ACLTokenCreation{}

	err := json.Unmarshal(extractedFiles[command.ConsulACLBootstrap], &aclBootstrapToken)
	if err != nil {
		return errors.WithMessagef(err, "unable to decode ACL Bootstrap token")
	}

	token, err := command.CreateACLToken(deps.CreateCommand, role, zimbraHostname, aclBootstrapToken.SecretID)
	if err != nil {
		return errors.WithMessagef(err, "unable to create ACL policy for this %s", role)
	}

	err = command.SetACLToken(deps.CreateCommand, token, aclBootstrapToken.SecretID)
	if err != nil {
		return err
	}

	return nil
}

// EnableSystemdUnitIfNotContainer enables the systemd unit if not running in a container.
func EnableSystemdUnitIfNotContainer(deps BusinessDependencies, isContainer bool) error {
	if !isContainer || TestingMode {
		err := systemd.EnableSystemdUnit(deps.SystemdUnitHandler, ServiceDiscoverUnit)
		if err != nil {
			return errors.Errorf("unable to enable %s unit: %s", ServiceDiscoverUnit, err)
		}
	}

	return nil
}
