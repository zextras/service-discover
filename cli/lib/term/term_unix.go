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

package term

import (
	"fmt"
	"golang.org/x/term"
	"io"
	"os"
)

const (
	DefaultTermPrompt = ""
	LineBreak         = "\n"
)

// NotATerminalError is an error that happens when the passed environment is not properly a terminal, and it is
// impossible to open it as it.
type NotATerminalError int

func (n NotATerminalError) Error() string {
	return fmt.Sprintf("the provided file descriptor (%d) is not a terminal", int(n))
}

// Terminal represents a terminal where information gets printed to the final user.
type Terminal interface {
	io.WriteCloser
	io.StringWriter
	ReadPassword(prompt string) (string, error)
	ReadLine() (string, error)
}

// terminal is an internal structure representing a terminal with takes inputs from os.Stdin
type terminal struct {
	term         *term.Terminal
	oldStateTerm *term.State
	stdIn        int
}

// MustRead simply checks that no error is present after the read. If it is, the function panics. Note that this
// function fails even if EOF is reached (e.g. an user pressed Ctrl+C while the program was waiting for an input)
func MustRead(out string, err error) string {
	if err != nil {
		panic(err)
	}
	return out
}

// MustWrite simply checks that no error is present after the write. If it is, the function panics
func MustWrite(bs int, err error) int {
	if err != nil {
		panic(err)
	}
	return bs
}

// Write writes down the array of bytes in the user terminal
func (t *terminal) Write(p []byte) (n int, err error) {
	return t.term.Write(p)
}

// WriteString writes down the given string in the user terminal
func (t *terminal) WriteString(s string) (n int, err error) {
	return fmt.Fprint(t.term, s)
}

// ReadPassword allows the user to enter a secret string without displaying it in the terminal (e.g. like `sudo` does)
func (t *terminal) ReadPassword(prompt string) (string, error) {
	return t.term.ReadPassword(prompt)
}

// ReadLine reads the current line until a line break is found
func (t *terminal) ReadLine() (string, error) {
	return t.term.ReadLine()
}

// Close restores the file descriptor provided initially to its initial status
func (t *terminal) Close() error {
	return term.Restore(t.stdIn, t.oldStateTerm)
}

// New returns a new terminal with the given reader and writer functions.
func New(reader *os.File, writer io.Writer, prompt string) (Terminal, error) {
	stdIn := int(reader.Fd())
	res := &terminal{
		stdIn: stdIn,
	}
	if !term.IsTerminal(res.stdIn) {
		return nil, NotATerminalError(stdIn)
	}
	var err error
	res.oldStateTerm, err = term.MakeRaw(res.stdIn)
	if err != nil {
		return nil, err
	}
	termIo := struct {
		io.Reader
		io.Writer
	}{reader, writer}
	res.term = term.NewTerminal(termIo, prompt)
	return res, nil
}
