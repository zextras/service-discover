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
	"bytes"
	"github.com/pkg/errors"
	"os/exec"
	"strings"
	"syscall"
)

// The Help command will redirect the user to a proper man page. If the user wants a quick help he can use --help.
// This command uses specific unix-syscall and for this is Unix only
type Help struct {
	Command `kong:"-"`
}

func (h *Help) Run(ctx *GlobalCommonFlags) error {
	var out bytes.Buffer
	manCmd := exec.Command("which", "man")
	manCmd.Stdout = &out
	err := manCmd.Run()
	if err != nil {
		return errors.New("unable to detect man package in this system. " +
			"Please install it in order to see detailed manual instructions")
	}
	args := make([]string, 1)
	args = append(args, h.applicationName)
	// We call exec directly otherwise exec.Command will perform "fork and run", we want to exec without run here.
	return syscall.Exec(strings.Trim(out.String(), "\n"), args, []string{}) // #nosec
}
