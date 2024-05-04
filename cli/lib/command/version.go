// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"fmt"
	"io"

	"github.com/Zextras/service-discover/cli/lib/formatter"
)

// The Version command prints out the version of the current application. Please note that, differently to --version
// flag, this command is more complete, gathering data to the agent, if it is available. The specification for this can
// be found in Confluence at the following page:
// https://zextras.atlassian.net/wiki/spaces/PROP/pages/1250230396/CLI+Guideline, code CLI-7
type Version struct {
	Command   `kong:"-"`
	writer    io.Writer `kong:"-"`
	agentName string    `kong:"-"`
}

// outputVersion is the output of the whole operation, and it is intended only for the Version command.
type outputVersion struct {
	ApplicationName string `json:"-"`
	CliVersion      string `json:"cli_version"`
	AgentName       string `json:"-"`
	AgentVersion    string `json:"agent_version"`
}

func (o *outputVersion) PlainRender() (string, error) {
	out := fmt.Sprintf("%s version: %s\n", o.ApplicationName, o.CliVersion)
	out += fmt.Sprintf("%s version: %s\n", o.AgentName, o.AgentVersion)
	return out, nil
}

func (o *outputVersion) JsonRender() (string, error) {
	return formatter.DefaultJsonRender(o)
}

func (v *Version) Run(globalFlags *GlobalCommonFlags) error {
	res := &outputVersion{
		v.applicationName,
		v.applicationVersion,
		v.agentName,
		"N/A", // TODO we have to implement a way to get the version of the Consul agent installed
	}
	out, err := formatter.Render(res, globalFlags.Format)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(v.writer, out)
	return err
}
