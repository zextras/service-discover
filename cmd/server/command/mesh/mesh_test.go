// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/mesh"
)

var errApply = errors.New("apply failed")

type ensured struct {
	parent, spiffeID, selector string
}

type fakeEnsurer struct {
	entries []ensured
	exist   map[string]bool
}

func (f *fakeEnsurer) EnsureEntry(parentID, spiffeID string, selectors ...string) (bool, error) {
	f.entries = append(f.entries, ensured{parentID, spiffeID, selectors[0]})

	return !f.exist[spiffeID], nil
}

type fixture struct {
	applied  [][]string
	ensurer  *fakeEnsurer
	out      *bytes.Buffer
	applyErr error
}

func (f *fixture) command(runsSpire bool, uid int, services ...string) *Apply {
	return &Apply{
		Service:     services,
		TrustDomain: "carbonio",
		writer:      f.out,
		getuid:      func() int { return uid },
		apply: func(_, _ string, names []string, _ io.Writer) error {
			f.applied = append(f.applied, names)

			return f.applyErr
		},
		runsSpire: func() bool { return runsSpire },
		hostname:  func() (string, error) { return "mail.example.com", nil },
		entries:   f.ensurer,
		definitions: func() ([]mesh.Definition, error) {
			return []mesh.Definition{
				{Name: "carbonio-files", User: "carbonio-files"},
				{Name: "carbonio-proxy", User: "zextras"},
			}, nil
		},
	}
}

func newFixture() *fixture {
	return &fixture{ensurer: &fakeEnsurer{exist: map[string]bool{}}, out: &bytes.Buffer{}}
}

func run(t *testing.T, cmd *Apply) error {
	t.Helper()

	t.Setenv(command.SetupConsulToken, "token")

	return cmd.Run(&command.GlobalCommonFlags{})
}

func TestApply_registersTheIdentitiesOfTheLocalServicesOnTheSpireServer(t *testing.T) {
	fix := newFixture()
	fix.ensurer.exist["spiffe://carbonio/service/carbonio-proxy"] = true
	cmd := fix.command(true, 0)
	cmd.bootstrap = &command.BootstrapToken{}

	require.NoError(t, run(t, cmd))

	assert.Equal(t, [][]string{nil}, fix.applied)
	assert.Equal(t, []ensured{
		{"spiffe://carbonio/node/mail.example.com", "spiffe://carbonio/service/carbonio-files", "unix:user:carbonio-files"},
		{"spiffe://carbonio/node/mail.example.com", "spiffe://carbonio/service/carbonio-proxy", "unix:user:zextras"},
	}, fix.ensurer.entries)
	assert.Contains(t, fix.out.String(), "1 identities of the services of mail.example.com registered")
}

func TestApply_onlyTheSelectedServices(t *testing.T) {
	fix := newFixture()
	cmd := fix.command(true, 0, "carbonio-files")
	cmd.bootstrap = &command.BootstrapToken{}

	require.NoError(t, run(t, cmd))

	assert.Equal(t, [][]string{{"carbonio-files"}}, fix.applied)
	require.Len(t, fix.ensurer.entries, 1)
	assert.Equal(t, "spiffe://carbonio/service/carbonio-files", fix.ensurer.entries[0].spiffeID)
}

func TestApply_unknownServiceIsAnError(t *testing.T) {
	fix := newFixture()
	cmd := fix.command(true, 0, "nope")
	cmd.bootstrap = &command.BootstrapToken{}

	require.ErrorIs(t, run(t, cmd), errUnknownService)
	assert.Empty(t, fix.ensurer.entries)
}

func TestApply_serversNotRunningSpireRegisterNoIdentity(t *testing.T) {
	fix := newFixture()
	cmd := fix.command(false, 0)
	cmd.bootstrap = &command.BootstrapToken{}

	require.NoError(t, run(t, cmd))

	assert.Len(t, fix.applied, 1)
	assert.Empty(t, fix.ensurer.entries)
	assert.Empty(t, fix.out.String())
}

func TestApply_failedRegistrationRegistersNoIdentity(t *testing.T) {
	fix := newFixture()
	fix.applyErr = errApply
	cmd := fix.command(true, 0)
	cmd.bootstrap = &command.BootstrapToken{}

	require.ErrorIs(t, run(t, cmd), errApply)
	assert.Empty(t, fix.ensurer.entries)
}

func TestApply_requiresRoot(t *testing.T) {
	fix := newFixture()
	cmd := fix.command(true, 1000)
	cmd.bootstrap = &command.BootstrapToken{}

	require.ErrorIs(t, run(t, cmd), errNotRoot)
	assert.Empty(t, fix.applied)
}
