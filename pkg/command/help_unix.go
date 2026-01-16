// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"syscall"

	"github.com/pkg/errors"
)

// The Help command will redirect the user to a proper man page. If the user wants a quick help he can use --help.
// This command uses specific unix-syscall and for this is Unix only.
type Help struct {
	Command `kong:"-"`
}

func (h *Help) Run(_ *GlobalCommonFlags) error {
	var out bytes.Buffer

	manCmd := exec.CommandContext(context.Background(), "which", "man")
	manCmd.Stdout = &out

	err := manCmd.Run()
	if err != nil {
		return errors.New("unable to detect man package in this system. " +
			"Please install it in order to see detailed manual instructions")
	}

	args := make([]string, 0, 2)
	args = append(args, "", h.applicationName)
	// We call exec directly otherwise exec.Command will perform "fork and run", we want to exec without run here.
	return syscall.Exec(strings.Trim(out.String(), "\n"), args, []string{}) // #nosec
}
