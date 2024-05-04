// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"fmt"
	"os"

	"github.com/Zextras/service-discover/cli/lib/command"
	"github.com/Zextras/service-discover/cli/server/config"
)

// The ServerFlags includes flags that are specific for the server CLI. In
// this case only the --version flag is included, since it is program
// specific.
type ServerFlags struct {
	command.GlobalCommonFlags
	Version versionFlag `help:"Show the version of this program" type:"bool"`
}

// versionFlag is a typedef for command.versionFlag in order to define a hook for the flag.
type versionFlag bool

// BeforeApply implementation in order to catch any --version and printing the version, as described in CLI-7.
func (g versionFlag) BeforeApply() error {
	fmt.Printf("%s %s\n", config.ApplicationName, config.ApplicationVersion)
	os.Exit(0)

	return nil
}
