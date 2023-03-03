package exec

import (
	mocks3 "github.com/Zextras/service-discover/cli/lib/exec/mocks"
	"github.com/Zextras/service-discover/cli/lib/test"
	"fmt"
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
