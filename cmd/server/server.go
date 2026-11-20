// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package main represents the main entrypoint of the whole agent CLI application
package main

import (
	"os"

	"github.com/alecthomas/kong"
	internalCommand "github.com/zextras/service-discover/cmd/server/command"
	"github.com/zextras/service-discover/cmd/server/command/jointoken"
	meshcommand "github.com/zextras/service-discover/cmd/server/command/mesh"
	"github.com/zextras/service-discover/cmd/server/command/setup"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/meshcmd"
	"github.com/zextras/service-discover/pkg/parser"
)

// The CLI represents the actual cli representation.
type CLI struct {
	internalCommand.ServerFlags

	Setup          setup.Setup            `cmd:"" help:"Perform first time setup of the server installation"`
	WizardSetup    setup.Wizard           `cmd:"" help:"Perform first time setup of the server installation in an interactive way" name:"setup-wizard"` //nolint:lll
	Config         command.Config         `cmd:"" help:"Manage service-discover configuration"`
	BootstrapToken command.BootstrapToken `cmd:"" help:"Print the bootstrap-token" name:"bootstrap-token"`
	ServiceToken   command.ServiceToken   `cmd:"" help:"Manage the Consul ACL token of a mesh service" name:"service-token"` //nolint:lll
	Spire          setup.Spire            `cmd:"" help:"Manage SPIRE on this server"`
	Mesh           meshcommand.Mesh       `cmd:"" help:"Register the Carbonio services in the service mesh"`
	JoinToken      jointoken.JoinToken    `cmd:"" help:"Generate the bundle to let a node join the cluster through SPIRE, without the cluster credentials" name:"join-token"` //nolint:lll

	Service meshcmd.Service `cmd:"" help:"Manage the credentials of the services installed on this node"`

	Version command.Version `cmd:"" help:"Show the version of this CLI and of the agent running in the host"`
	Help    command.Help    `cmd:"" help:"Print the program help"`
}

func main() {
	cmd := command.NewCommand(
		config.ApplicationName,
		config.ApplicationVersion,
	)
	s := setup.NewSetup()
	bootstrapToken := cmd.BootstrapToken(os.Stdout, config.AgentName)
	cli := &CLI{
		Setup:       s,
		WizardSetup: setup.NewWizardSetup(&s),
		Config: cmd.Config(
			os.Stdout,
			config.AgentName,
		),
		BootstrapToken: cmd.BootstrapToken(
			os.Stdout,
			config.AgentName,
		),
		Service: meshcmd.New(os.Stdout),
		Version: cmd.Version(
			os.Stdout,
			config.AgentName,
		),
		Spire:     setup.NewSpire(),
		JoinToken: jointoken.New(os.Stdout),
		Mesh:      meshcommand.New(&bootstrapToken, os.Stdout),
		Help:      cmd.Help(),
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
