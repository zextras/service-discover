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

package command

import (
	"fmt"
	"github.com/Zextras/service-discover/cli/lib/formatter"
	"io"
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
