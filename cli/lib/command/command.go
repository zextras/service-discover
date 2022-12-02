package command

import (
	"io"
	"os"
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
		*c,
		writer,
		agentName,
		false,
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
