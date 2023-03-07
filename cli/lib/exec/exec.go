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

package exec

import (
	"fmt"
	"github.com/pkg/errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type Cmd interface {
	String() string
	Run() error
	Start() error
	Wait() error
	Output() ([]byte, error)
	CombinedOutput() ([]byte, error)
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
}

func Command(name string, arg ...string) Cmd {
	return exec.Command(name, arg...)
}

// InPath change directory in the desired path before executing the desired command. It then proceed to return to
// the original folder
func InPath(cmd Cmd, path string) error {
	executionPath, err := os.Executable()
	if err != nil {
		return err
	}
	originalPath := filepath.Dir(executionPath)
	if err = os.Chdir(path); err != nil {
		return err
	}
	_, err = cmd.Output()
	if err != nil {
		return err
	}
	if err = os.Chdir(originalPath); err != nil {
		return err
	}
	return nil
}

// ErrorFromStderr extract the error got from stderr and it appends it after the desired reason.
func ErrorFromStderr(err error, reason string) error {
	stderr := err.Error()
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		stderr = string(ee.Stderr)
	}
	return errors.New(fmt.Sprintf("%s: %s", reason, stderr))
}
