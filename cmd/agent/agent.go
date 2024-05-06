// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package main represents the main entrypoint of the whole agent CLI application
package main

import (
	"os"

	"github.com/alecthomas/kong"
	internalCommand "github.com/zextras/service-discover/cmd/agent/command"
	"github.com/zextras/service-discover/cmd/agent/command/setup"
	"github.com/zextras/service-discover/cmd/agent/config"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/parser"
)

// The CLI represents the actual cli representation.
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
