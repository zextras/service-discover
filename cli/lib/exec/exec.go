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

// ExecInPath change directory in the desired path before executing the desired command. It then proceed to return to
// the original folder
func ExecInPath(cmd Cmd, path string) error {
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
