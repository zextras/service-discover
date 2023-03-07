/*
 * Copyright (C) 2023 Zextras srl
 *
 *     This program is free software: you can redistribute it and/or modify
 *     it under the terms of the GNU Affero General Public License as published by
 *     the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU Affero General Public License for more details.
 *
 *     You should have received a copy of the GNU Affero General Public License
 *     along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 */

// Package main represents the main entrypoint of the whole agent CLI application
package main

import (
	internalCommand "github.com/Zextras/service-discover/cli/agent/command"
	"github.com/Zextras/service-discover/cli/agent/command/setup"
	"github.com/Zextras/service-discover/cli/agent/config"
	"github.com/Zextras/service-discover/cli/lib/command"
	"github.com/Zextras/service-discover/cli/lib/parser"
	"github.com/alecthomas/kong"
	"os"
)

// The CLI represents the actual cli representation
type CLI struct {
	internalCommand.AgentFlags

	Setup          setup.Setup            `cmd help:"Run first time setup for an agent node"`
	SetupWizard    setup.Wizard           `cmd help:"Run first time setup for an agent node in an interactive way" name:"setup-wizard"`
	Config         command.Config         `cmd help:"Manage service-discover configuration"`
	BootstrapToken command.BootstrapToken `cmd help:"Print the bootstrap-token" name:"bootstrap-token"`

	Version command.Version `cmd help:"Show the version of this CLI and of the agent running in the host"`
	Help    command.Help    `cmd help:"Print the program help"`
}

func main() {
	cmd := command.NewCommand(
		config.ApplicationName,
		config.ApplicationVersion,
	)
	s := setup.New()
	cli := &CLI{
		Setup:       s,
		SetupWizard: setup.NewWizardSetup(&s),
		Config: cmd.Config(
			os.Stdout,
			config.AgentName,
		),
		BootstrapToken: cmd.BootstrapToken(
			os.Stdout,
			config.AgentName,
		),
		Version: cmd.Version(
			os.Stdout,
			config.AgentName,
		),
		Help: cmd.Help(),
	}
	ctx := parser.Parse(cli,
		kong.Name(config.ApplicationName),
		kong.Description(config.ApplicationDescription),
		kong.UsageOnError(),
	)
	err := ctx.Validate()
	if err != nil {
		panic(err)
	}
	ctx.FatalIfErrorf(ctx.Run(&cli.GlobalCommonFlags))
}
