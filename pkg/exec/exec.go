// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package exec

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pkg/errors"
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
	return exec.CommandContext(context.Background(), name, arg...)
}

// InPath change directory in the desired path before executing the desired command. It then proceed to return to
// the original folder.
func InPath(cmd Cmd, path string) error {
	executionPath, err := os.Executable()
	if err != nil {
		return err
	}

	originalPath := filepath.Dir(executionPath)

	err = os.Chdir(path)
	if err != nil {
		return err
	}

	_, err = cmd.Output()
	if err != nil {
		return err
	}

	err = os.Chdir(originalPath)
	if err != nil {
		return err
	}

	return nil
}

// ErrorFromStderr extract the error got from stderr and it appends it after the desired reason.
func ErrorFromStderr(err error, reason string) error {
	stderr := err.Error()

	ee := &exec.ExitError{}
	if errors.As(err, &ee) {
		stderr = string(ee.Stderr)
	}

	return errors.Errorf("%s: %s", reason, stderr)
}
