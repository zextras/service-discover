// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"io"
	"os"

	term2 "github.com/zextras/service-discover/pkg/term"
)

// The Command struct holds all the common data between all the different commands. It hold base information like the
// application name and the application version.
type Command struct {
	applicationName    string `kong:"-"`
	applicationVersion string `kong:"-"`
}

func NewCommand(applicationName string, applicationVersion string) *Command {
	return &Command{
		applicationName:    applicationName,
		applicationVersion: applicationVersion,
	}
}

// Version generates a Version structure that can is ready to be integrated as CLI command
func (c *Command) Version(writer io.Writer, agentName string) Version {
	return Version{
		*c,
		writer,
		agentName,
	}
}

// BootstrapToken generates a BootstrapToken structure that can is ready to be integrated as CLI command
func (c *Command) BootstrapToken(writer io.Writer, agentName string) BootstrapToken {
	return BootstrapToken{
		Command:                       *c,
		writer:                        writer,
		agentName:                     agentName,
		Setup:                         false,
		Password:                      "",
		clusterCredentialFileLocation: "/etc/zextras/service-discover/cluster-credentials.tar.gpg",
		termUiProvider:                &term2.TermUiProvider{},
	}
}

// Help generates a Help command that open up the man pages on unix compatible machines. The command will always be
// "man <applicationName>"
func (c *Command) Help() Help {
	return Help{*c}
}

func (c *Command) Config(writer io.Writer, agentName string) Config {
	return Config{
		Command:   *c,
		writer:    writer,
		agentName: agentName,
		Get: GetConfig{
			Command:   *c,
			ReadFile:  os.ReadFile,
			writer:    writer,
			agentName: agentName,
		},
		Set: SetConfig{
			Command:   *c,
			ReadFile:  os.ReadFile,
			WriteFile: os.WriteFile,
			writer:    writer,
			agentName: agentName,
		},
		List: ListConfig{
			Command:   *c,
			writer:    writer,
			agentName: agentName,
		},
	}
}
