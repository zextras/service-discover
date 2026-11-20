// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pkgexec "github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/exec/mocks"
	"github.com/zextras/service-discover/pkg/spire"
)

const prettyJWTOutput = "token(spiffe://carbonio/consul/agent-h):\n\teyJhbGciOi.payload.sig\t\nbundle(spiffe://carbonio):\n\t{}\n"

func TestAgentCLI_fetchJWTSVID(t *testing.T) {
	t.Parallel()

	var gotArgs []string

	cli := spire.NewAgentCLI(func(name string, args ...string) pkgexec.Cmd {
		assert.Equal(t, spire.AgentBin, name)

		gotArgs = args
		cmd := new(mocks.Cmd)
		cmd.On("Output").Return([]byte(prettyJWTOutput), nil)

		return cmd
	})

	token, err := cli.FetchJWTSVID(spire.AudienceAutoConfig, spire.ConsulID("agent-h"))
	require.NoError(t, err)
	assert.Equal(t, "eyJhbGciOi.payload.sig", token)
	assert.Equal(t, []string{"api", "fetch", "jwt", "-audience", "consul-auto-config",
		"-spiffeID", "spiffe://carbonio/consul/agent-h", "-socketPath", spire.AgentSocket}, gotArgs)
}

func TestAgentCLI_fetchJWTSVIDErrors(t *testing.T) {
	t.Parallel()

	for name, out := range map[string]string{"empty": "", "no token line": "token(x):\n", "wrong header": "foo\nbar\n"} {
		cli := spire.NewAgentCLI(func(string, ...string) pkgexec.Cmd {
			cmd := new(mocks.Cmd)
			cmd.On("Output").Return([]byte(out), nil)

			return cmd
		})

		_, err := cli.FetchJWTSVID("a", "b")
		require.Error(t, err, name)
	}

	cli := spire.NewAgentCLI(func(string, ...string) pkgexec.Cmd {
		cmd := new(mocks.Cmd)
		cmd.On("Output").Return(nil, &exec.ExitError{Stderr: []byte("no identity issued")})

		return cmd
	})

	_, err := cli.FetchJWTSVID("a", "b")
	require.ErrorContains(t, err, "no identity issued")
}

func TestAgentCLI_waitJWTSVID(t *testing.T) {
	t.Parallel()

	attempts := 0
	cli := spire.NewAgentCLI(func(string, ...string) pkgexec.Cmd {
		attempts++

		cmd := new(mocks.Cmd)
		if attempts < 3 {
			cmd.On("Output").Return(nil, &exec.ExitError{Stderr: []byte("connection refused")})
		} else {
			cmd.On("Output").Return([]byte(prettyJWTOutput), nil)
		}

		return cmd
	})

	token, err := cli.WaitJWTSVID("a", "b", time.Second, time.Millisecond)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Equal(t, 3, attempts)

	failing := spire.NewAgentCLI(func(string, ...string) pkgexec.Cmd {
		cmd := new(mocks.Cmd)
		cmd.On("Output").Return(nil, &exec.ExitError{Stderr: []byte("connection refused")})

		return cmd
	})

	_, err = failing.WaitJWTSVID("a", "b", 20*time.Millisecond, 5*time.Millisecond)
	require.ErrorContains(t, err, "Workload API is not ready")
}
