// Package main represents the main entrypoint of the whole agent CLI application
package main

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/parser"
	internalCommand "bitbucket.org/zextras/service-discover/cli/server/command"
	"bitbucket.org/zextras/service-discover/cli/server/command/setup"
	"bitbucket.org/zextras/service-discover/cli/server/config"
	"github.com/alecthomas/kong"
	"os"
)

// The CLI represents the actual cli representation
type CLI struct {
	internalCommand.ServerFlags

	Setup       setup.Setup  `cmd help:"Perform first time setup of the server installation"`
	WizardSetup setup.Wizard `cmd help:"Perform first time setup of the server installation in an interactive way" name:"setup-wizard"`

	Config  command.Config  `cmd help:"Manage service-discover configuration"`
	Version command.Version `cmd help:"Show the version of this CLI and of the agent running in the host"`
	Help    command.Help    `cmd help:"Print the program help"`
}

func main() {
	cmd := command.NewCommand(
		config.ApplicationName,
		config.ApplicationVersion,
	)
	s := setup.NewSetup()
	cli := &CLI{
		Setup: s,
		WizardSetup: setup.NewWizardSetup(&s),
		Config: cmd.Config(
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
