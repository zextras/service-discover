// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/legacytoken"
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

// revoker plays legacytoken.RevokeLegacyServiceTokens over the services files, proxy and unknown: it asks the selector
// about each one and revokes the token of the services it accepts.
type revoker struct {
	selector legacytoken.Selector
	called   bool
	err      error
}

func (r *revoker) revoke(_ string, selector legacytoken.Selector) ([]legacytoken.Token, []legacytoken.Kept, error) {
	r.called = true
	r.selector = selector

	var (
		revoked []legacytoken.Token
		kept    []legacytoken.Kept
	)

	for _, service := range []string{"carbonio-files", "carbonio-proxy", "carbonio-unknown"} {
		ok, err := selector.ServiceRegistered(service)
		if err != nil {
			return nil, nil, err
		}

		token := legacytoken.Token{AccessorID: "id-" + service, Service: service}
		if ok {
			revoked = append(revoked, token)
		} else {
			kept = append(kept, legacytoken.Kept{Token: token, Reason: "no login token yet"})
		}
	}

	return revoked, kept, r.err
}

func (f *fixture) revoking(registered map[string]bool, withLogin ...string) (*Apply, *revoker) {
	cmd := f.command(true, rootUID)
	cmd.RevokeLegacy = true

	rev := &revoker{}
	cmd.revoke = rev.revoke
	cmd.hasEntry = func(parentID, spiffeID string) (bool, error) {
		return registered[parentID+" "+spiffeID], nil
	}
	cmd.loginInPlace = func(string) (func(*mesh.Definition) bool, error) {
		return func(def *mesh.Definition) bool { return slices.Contains(withLogin, def.Name) }, nil
	}

	return cmd, rev
}

func TestApply_revokeLegacyOnlyForTheServicesWithALoginToken(t *testing.T) {
	fix := newFixture()
	registered := map[string]bool{
		"spiffe://carbonio/node/mail.example.com spiffe://carbonio/service/carbonio-files": true,
		"spiffe://carbonio/node/mail.example.com spiffe://carbonio/service/carbonio-proxy": true,
	}

	// The proxy is registered in SPIRE but its login token is not in place yet.
	cmd, rev := fix.revoking(registered, "carbonio-files")

	require.NoError(t, run(t, cmd))

	assert.True(t, rev.called)
	assert.Equal(t, "mail.example.com", rev.selector.Hostnames[0])
	assert.Equal(t, command.ConsulNodeName(command.Server, "mail.example.com"), rev.selector.NodeName)

	out := fix.out.String()
	assert.Contains(t, out, "Legacy ACL token id-carbonio-files of carbonio-files deleted")
	assert.Contains(t, out, "Legacy ACL token id-carbonio-proxy of carbonio-proxy kept: no login token yet")
	assert.Contains(t, out, "Legacy ACL token id-carbonio-unknown of carbonio-unknown kept")
	assert.Contains(t, out, "Legacy ACL tokens: 1 deleted, 2 kept")
}

func TestApply_revokeLegacyNeedsTheIdentityInSpire(t *testing.T) {
	fix := newFixture()

	cmd, _ := fix.revoking(map[string]bool{}, "carbonio-files")

	require.NoError(t, run(t, cmd))
	assert.Contains(t, fix.out.String(), "Legacy ACL tokens: 0 deleted, 3 kept")
}

func TestApply_legacyTokensAreKeptWithoutTheFlag(t *testing.T) {
	fix := newFixture()

	cmd, rev := fix.revoking(map[string]bool{}, "carbonio-files")
	cmd.RevokeLegacy = false

	require.NoError(t, run(t, cmd))
	assert.False(t, rev.called)
}

func TestApply_noRevocationWhenTheRegistrationFailed(t *testing.T) {
	fix := newFixture()
	fix.applyErr = errApply

	cmd, rev := fix.revoking(map[string]bool{}, "carbonio-files")

	require.ErrorIs(t, run(t, cmd), errApply)
	assert.False(t, rev.called)
}

func TestApply_revokeLegacyReportsTheFailure(t *testing.T) {
	fix := newFixture()

	cmd, rev := fix.revoking(map[string]bool{}, "carbonio-files")
	rev.err = errApply

	require.ErrorIs(t, run(t, cmd), errApply)
	assert.NotContains(t, fix.out.String(), "Legacy ACL tokens:")
}

func TestInstalledManifest_readsTheManifestOfTheSpecsDirectory(t *testing.T) { //nolint:paralleltest // changes the environment
	dir := t.TempDir()
	t.Setenv(mesh.SpecsDirEnv, dir)

	_, err := installedManifest()
	require.ErrorIs(t, err, mesh.ErrSpecsMissing)

	require.NoError(t, os.WriteFile(filepath.Join(dir, mesh.ManifestFile),
		[]byte(`{"name":"service-discover-specs","version":"1.2.3-1","services":2,"tree_sha256":"abc"}`), 0o600))

	manifest, err := installedManifest()

	require.NoError(t, err)
	assert.Equal(t, "1.2.3-1", manifest.Version)
}
