// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"io"
	"os"
	osexec "os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/spire"
)

// finalizeCovDeps fakes the consul and spire-server binaries for finalizeAgent, with a node and a service of the host
// to revoke; the commands selected by finalizeCovCase.fail fail.
type finalizeCovDeps struct {
	*flowDeps

	uid int
}

func (d *finalizeCovDeps) GetuidSyscall() int { return d.uid }

type finalizeCovCase struct {
	// fail maps a part of the command line to the nth call (1-based, 0 meaning every call) that has to fail.
	fail map[string]int
	uid  int
}

func newFinalizeCovDeps(t *testing.T, tc finalizeCovCase) *finalizeCovDeps {
	t.Helper()

	calls := map[string]int{}
	deps := &finalizeCovDeps{uid: tc.uid}
	deps.flowDeps = &flowDeps{handler: func(args []string) ([]byte, error) {
		line := strings.Join(args, " ")

		for part, nth := range tc.fail {
			if !strings.Contains(line, part) {
				continue
			}

			calls[part]++

			if nth == 0 || nth == calls[part] {
				return nil, &osexec.ExitError{Stderr: []byte("failed: " + line)}
			}
		}

		switch {
		case strings.Contains(line, "acl token list"):
			return []byte("[" + legacyToken + "," + autoToken + "," + serviceToken + "," + filesLogin + "]"), nil
		case strings.Contains(line, "acl policy list"):
			return []byte(policyList), nil
		case strings.HasSuffix(line, "consul members"):
			return []byte(membersHeader + finalizeNode + " 10.0.0.9:8301 alive client 2.1.0 2 dc1 default <default>\n"), nil
		case strings.Contains(line, spire.ServerBin+" entry show"):
			return []byte(`{"entries":[{"id":"1"}]}`), nil
		}

		return []byte("ok"), nil
	}}

	return deps
}

func TestSetup_finalizeAgent_failures(t *testing.T) {
	tests := []struct {
		name    string
		tc      finalizeCovCase
		secrets func() (clusterSecrets, error)
		wantErr string
	}{
		{name: "not root", tc: finalizeCovCase{uid: 1000}, wantErr: "must be executed as root"},
		{name: "invalid token", tc: finalizeCovCase{},
			secrets: func() (clusterSecrets, error) { return clusterSecrets{token: "bad\x00token"}, nil },
			wantErr: "unable to set the Consul token"},
		{name: "tokens not listed", tc: finalizeCovCase{fail: map[string]int{"acl token list": 1}},
			wantErr: "unable to run consul acl token list"},
		{name: "policies not listed", tc: finalizeCovCase{fail: map[string]int{"acl policy list": 0}},
			wantErr: "unable to run consul acl policy list"},
		{name: "service tokens not listed", tc: finalizeCovCase{fail: map[string]int{"acl token list": 2}},
			wantErr: "unable to run consul acl token list"},
		{name: "SPIRE entries not listed", tc: finalizeCovCase{fail: map[string]int{spire.ServerBin + " entry show": 0}},
			wantErr: "unable to list the SPIRE entries"},
		{name: "members not listed", tc: finalizeCovCase{fail: map[string]int{"consul members": 0}},
			wantErr: "unable to list the members of the cluster"},
		{name: "legacy token not deleted", tc: finalizeCovCase{fail: map[string]int{"token delete -id legacy-1": 0}},
			wantErr: "unable to delete the ACL token legacy-1"},
		{name: "policy not deleted", tc: finalizeCovCase{fail: map[string]int{"policy delete": 0}},
			wantErr: "unable to delete the ACL policy " + finalizeNode},
		{name: "service token not deleted", tc: finalizeCovCase{fail: map[string]int{"token delete -id svc-files": 0}},
			wantErr: "unable to delete the ACL token svc-files"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newFinalizeCovDeps(t, tt.tc)
			secrets := tt.secrets

			if secrets == nil {
				secrets = finalizeSecrets
			}

			_, err := (&Setup{}).finalizeAgent(deps, "mail2.example.com", secrets)

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, os.Getenv(command.ConsulHTTPToken), "the token must not leak")
		})
	}
}

func TestSetup_finalizeAgent_undecodableAnswer(t *testing.T) {
	deps := newFinalizeCovDeps(t, finalizeCovCase{})
	handler := deps.handler
	deps.handler = func(args []string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "acl policy list") {
			return []byte("not json"), nil
		}

		return handler(args)
	}

	_, err := (&Setup{}).finalizeAgent(deps, "mail2.example.com", finalizeSecrets)

	require.ErrorContains(t, err, "unable to decode the answer of consul")
}

func TestSetup_finalizeAgent_reportsWhatWasDeletedBeforeAServiceTokenFailure(t *testing.T) {
	deps := newFinalizeCovDeps(t, finalizeCovCase{fail: map[string]int{"token delete -id svc-files": 0}})

	_, err := (&Setup{}).finalizeAgent(deps, "mail2.example.com", finalizeSecrets)

	require.Error(t, err)
	assert.True(t, deps.ranCommand(command.ConsulBin+" acl token delete -id legacy-1"))
	assert.True(t, deps.ranCommand(command.ConsulBin+" acl policy delete -name "+finalizeNode))
}

func TestFinalize_Run(t *testing.T) {
	// root is not available when testing: finalizeAgent refuses to go on, whoever the user running the tests is,
	// before anything is rendered
	finalize := &Finalize{Host: "mail2.example.com", Password: "pw"}

	err := finalize.Run(&command.GlobalCommonFlags{Format: formatter.PlainFormatOutput})

	require.Error(t, err)
}

func TestFinalize_Run_asksThePassword(t *testing.T) {
	withStdin := func(t *testing.T, input string) {
		t.Helper()

		reader, writer, err := os.Pipe()
		require.NoError(t, err)

		_, err = writer.WriteString(input)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		previous := os.Stdin
		os.Stdin = reader

		t.Cleanup(func() {
			os.Stdin = previous

			reader.Close()
		})
	}

	flags := &command.GlobalCommonFlags{Format: formatter.PlainFormatOutput}

	t.Run("the password comes from the input", func(t *testing.T) {
		withStdin(t, "pw\n")

		err := (&Finalize{Host: "mail2.example.com"}).Run(flags)

		require.Error(t, err, "finalizeAgent cannot succeed when testing")
		assert.NotErrorIs(t, err, io.EOF, "the password was read")
	})

	t.Run("no password to read", func(t *testing.T) {
		withStdin(t, "")

		err := (&Finalize{Host: "mail2.example.com"}).Run(flags)

		require.ErrorIs(t, err, io.EOF)
	})
}
