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
	"github.com/Zextras/service-discover/cli/agent/config"
	"github.com/Zextras/service-discover/cli/lib/command"
	"os"
)

// The AgentFlags includes flags that are specific for the agent CLI. In this case only the --version flag is included,
// since it is program specific
type AgentFlags struct {
	command.GlobalCommonFlags
	Version versionFlag `help:"Show the version of this program" type:"bool"`
}

// versionFlag is a typedef for command.versionFlag in order to define a hook for the flag
type versionFlag bool

// BeforeApply implementation in order to catch any --version and printing the version, as described in CLI-7
func (g versionFlag) BeforeApply() error {
	fmt.Printf("%s %s\n", config.ApplicationName, config.ApplicationVersion)
	os.Exit(0)
	return nil
}
