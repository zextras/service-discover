// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package term

import (
	"bytes"
	"io"
)

// lineTerminal is the Terminal used when the input is not a terminal: secrets and lines are plain lines of the input,
// and prompts are not printed since nobody is there to read them.
type lineTerminal struct {
	reader io.Reader
	writer io.Writer
}

func newLineTerminal(reader io.Reader, writer io.Writer) Terminal {
	return &lineTerminal{reader: reader, writer: writer}
}

func (t *lineTerminal) Write(p []byte) (int, error) {
	return t.writer.Write(p)
}

func (t *lineTerminal) WriteString(s string) (int, error) {
	return io.WriteString(t.writer, s)
}

// ReadPassword reads the next line of the input; the prompt is ignored.
func (t *lineTerminal) ReadPassword(string) (string, error) {
	return t.ReadLine()
}

// ReadLine reads the next line of the input without its line break. The input is read one byte at a time, so that
// nothing past the line is consumed: another terminal created later on the same input finds the rest of it. A last
// line without a line break is returned as it is, the end of the input when there is nothing left.
func (t *lineTerminal) ReadLine() (string, error) {
	var (
		line bytes.Buffer
		char [1]byte
	)

	for {
		count, err := t.reader.Read(char[:])
		if count == 1 {
			if char[0] == '\n' {
				return string(bytes.TrimSuffix(line.Bytes(), []byte("\r"))), nil
			}

			line.WriteByte(char[0])
		}

		if err != nil {
			if err == io.EOF && line.Len() > 0 { //nolint:errorlint // io.EOF is returned as it is
				return line.String(), nil
			}

			return "", err
		}
	}
}

// Close does nothing: the input was not modified.
func (t *lineTerminal) Close() error {
	return nil
}
