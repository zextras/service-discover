package command

import "io"

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

// Help generates a Help command that open up the man pages on unix compatible machines. The command will always be
// "man <applicationName>"
func (c *Command) Help() Help {
	return Help{*c}
}
