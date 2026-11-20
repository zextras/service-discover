// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package term

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_New_pipedInput(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	defer reader.Close()

	_, err = writer.WriteString("secret\r\nsecond\nlast")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	var out bytes.Buffer

	terminal, err := New(reader, &out, DefaultTermPrompt)
	require.NoError(t, err)

	password, err := terminal.ReadPassword("Insert the cluster credential password: ")
	require.NoError(t, err)
	assert.Equal(t, "secret", password)

	// a second terminal on the same input finds the rest of it
	other, err := New(reader, &out, DefaultTermPrompt)
	require.NoError(t, err)

	line, err := other.ReadLine()
	require.NoError(t, err)
	assert.Equal(t, "second", line)

	line, err = terminal.ReadLine()
	require.NoError(t, err)
	assert.Equal(t, "last", line)

	_, err = terminal.ReadLine()
	assert.ErrorIs(t, err, io.EOF)

	require.NoError(t, terminal.Close())
	assert.Empty(t, out.String(), "no prompt is printed on piped input")
}

func Test_lineTerminal_writes(t *testing.T) {
	var out bytes.Buffer

	terminal := newLineTerminal(bytes.NewReader(nil), &out)

	count, err := terminal.Write([]byte("abc"))
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	count, err = terminal.WriteString("def")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	assert.Equal(t, "abcdef", out.String())
	require.NoError(t, terminal.Close())
}
