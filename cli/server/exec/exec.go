package exec

import (
	"io"
	"os/exec"
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
