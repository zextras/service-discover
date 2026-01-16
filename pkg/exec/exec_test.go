// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package exec

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	mocks3 "github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/test"
)

func TestCommand(t *testing.T) {
	t.Parallel()

	t.Run("Command returns a valid Cmd", func(t *testing.T) {
		cmd := Command("echo", "hello")
		assert.NotNil(t, cmd)
		// Command path may vary by system, just check it contains the command
		assert.Contains(t, cmd.String(), "echo")
		assert.Contains(t, cmd.String(), "hello")
	})

	t.Run("Command can execute successfully", func(t *testing.T) {
		cmd := Command("echo", "test")
		output, err := cmd.Output()
		assert.NoError(t, err)
		assert.Contains(t, string(output), "test")
	})
}

func TestExecInPath(t *testing.T) {
	t.Parallel()

	t.Run("Error if chdir in a non-existing dir", func(t *testing.T) {
		nonExistingFolder := test.GenerateRandomFolder("Error if chdir in a non-existing dir")
		assert.NoError(t, os.RemoveAll(nonExistingFolder))
		assert.EqualError(
			t,
			InPath(exec.Command("it", "doesn't", "matter"), nonExistingFolder),
			fmt.Sprintf("chdir %s: no such file or directory", nonExistingFolder),
		)
	})

	t.Run("Error is reported if command fails", func(t *testing.T) {
		mockCmd := new(mocks3.Cmd)
		mockCmd.On("Output").Return(nil, errors.New("this is an error message"))

		existingFolder := test.GenerateRandomFolder("Works correctly in an existing dir")

		defer os.RemoveAll(existingFolder)
		assert.EqualError(
			t,
			InPath(mockCmd, existingFolder),
			"this is an error message",
		)
	})

	t.Run("Works correctly in an existing dir", func(t *testing.T) {
		mockCmd := new(mocks3.Cmd)
		mockCmd.On("Output").Return([]byte("something"), nil)

		existingFolder := test.GenerateRandomFolder("Works correctly in an existing dir")

		defer os.RemoveAll(existingFolder)
		assert.NoError(t, InPath(mockCmd, existingFolder))
	})
}
