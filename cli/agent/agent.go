// Package main represents the main entrypoint of the whole agent CLI application
package main

import (
	internalCommand "bitbucket.org/zextras/service-discover/cli/agent/command"
	"bitbucket.org/zextras/service-discover/cli/agent/config"
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/parser"
	"github.com/alecthomas/kong"
	"os"
)

// The CLI represents the actual cli representation
type CLI struct {
	internalCommand.AgentFlags

	Config  command.Config  `cmd help:"Manage service-discover configuration"`
	Version command.Version `cmd help:"Show the version of this CLI and of the agent running in the host"`
	Help    command.Help    `cmd help:"Print the program help"`
}

func main() {
	cmd := command.NewCommand(
		config.ApplicationName,
		config.ApplicationVersion,
	)
	cli := &CLI{
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
