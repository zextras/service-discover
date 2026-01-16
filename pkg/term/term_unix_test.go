// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package term

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUIProviderImpl_Get(t *testing.T) {
	t.Parallel()

	t.Run("UIProviderImpl returns terminal successfully", func(t *testing.T) {
		provider := &UIProviderImpl{}
		writer := &bytes.Buffer{}

		// This test may fail if stdin is not a terminal, which is expected in CI
		terminal, err := provider.Get(writer)
		if err != nil {
			// If we're not in a terminal (CI environment), we expect NotATerminalError
			_, ok := err.(NotATerminalError)
			assert.True(t, ok, "Expected NotATerminalError when not in terminal")
		} else {
			// If we are in a terminal, the result should be valid
			assert.NotNil(t, terminal)
			assert.NoError(t, terminal.Close())
		}
	})
}

func TestNotATerminalError(t *testing.T) {
	t.Parallel()

	t.Run("NotATerminalError returns correct message", func(t *testing.T) {
		err := NotATerminalError(5)
		assert.Equal(t, "the provided file descriptor (5) is not a terminal", err.Error())
	})
}

func TestMustRead(t *testing.T) {
	t.Parallel()

	t.Run("MustRead returns value when no error", func(t *testing.T) {
		result := MustRead("test", nil)
		assert.Equal(t, "test", result)
	})

	t.Run("MustRead panics on error", func(t *testing.T) {
		assert.Panics(t, func() {
			MustRead("", assert.AnError)
		})
	})
}

func TestMustWrite(t *testing.T) {
	t.Parallel()

	t.Run("MustWrite returns value when no error", func(t *testing.T) {
		result := MustWrite(10, nil)
		assert.Equal(t, 10, result)
	})

	t.Run("MustWrite panics on error", func(t *testing.T) {
		assert.Panics(t, func() {
			MustWrite(0, assert.AnError)
		})
	})
}
