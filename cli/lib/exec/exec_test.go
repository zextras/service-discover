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
	mocks3 "github.com/Zextras/service-discover/cli/lib/exec/mocks"
	"github.com/Zextras/service-discover/cli/lib/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"os"
	"os/exec"
	"testing"
)

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
