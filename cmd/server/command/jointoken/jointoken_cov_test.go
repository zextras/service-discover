// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package jointoken

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/exec/mocks"
)

var errCreateEntry = errors.New("entry create failed")

// secondEntryFailingDeps lets the entry of the node pass and fails the creation of the entries of its services.
type secondEntryFailingDeps struct {
	fakeDeps

	creates int
}

func (f *secondEntryFailingDeps) CreateCommand(name string, args ...string) exec.Cmd {
	if args[0]+" "+args[1] != "entry create" {
		return f.fakeDeps.CreateCommand(name, args...)
	}

	f.creates++
	if f.creates == 1 {
		return f.fakeDeps.CreateCommand(name, args...)
	}

	cmd := new(mocks.Cmd)
	cmd.On("Output").Return(nil, errCreateEntry)

	return cmd
}

func TestJoinToken_failedRegistrationOfTheServicesBurnsNoToken(t *testing.T) {
	t.Parallel()

	deps := &secondEntryFailingDeps{fakeDeps: *newDeps(`{"entries":[]}`)}
	cmd, out := newCommand(t, &fakeDeps{}, "mail2.example.com")
	cmd.deps = deps

	err := cmd.Run(&command.GlobalCommonFlags{})

	require.ErrorContains(t, err, errCreateEntry.Error())
	require.ErrorContains(t, err, "unable to register spiffe://")
	assert.NotContains(t, deps.calls, "token generate", "the token is generated after the entries")
	assert.Empty(t, out.String())
}
