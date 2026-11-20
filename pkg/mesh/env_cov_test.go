// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/spire"
)

// envFakeScript writes an executable shell script and returns its path.
func envFakeScript(t *testing.T, dir, name, body string) string {
	t.Helper()

	script := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0o700)) //nolint:gosec // test script

	return script
}

func TestNewEnv_wiresTheRealMachine(t *testing.T) {
	stub := newMeshConsul(t, map[string]meshReply{
		"POST /v1/acl/login":  {meshOK, `{"SecretID":"issued"}`},
		"POST /v1/acl/logout": {meshOK, ``},
		meshTokenSelf:         {meshOK, `{"AuthMethod":"spire"}`},
	})

	env, err := NewEnv("carbonio.lab")
	require.NoError(t, err)

	assert.Equal(t, "carbonio.lab", env.TrustDomain)
	assert.Equal(t, ServicesDir, env.ServicesDir)
	assert.Equal(t, RenderedDir, env.RenderedDir)
	assert.Equal(t, StateDir, env.StateDir)
	assert.Equal(t, CarbonioDir, env.CarbonioDir)
	assert.Equal(t, SystemdDir, env.SystemdDir)
	assert.Equal(t, CertDir, env.CertDir)

	secret, err := env.Login("jwt")
	require.NoError(t, err)
	assert.Equal(t, "issued", secret)
	assert.Contains(t, stub.last(t).body, `"AuthMethod":"`+spire.AuthMethodName+`"`)

	valid, err := env.Valid("issued")
	require.NoError(t, err)
	assert.True(t, valid)

	isLogin, err := env.IsLogin("issued")
	require.NoError(t, err)
	assert.True(t, isLogin)

	require.NoError(t, env.Logout("issued"))

	service, err := env.Consul("svc-token")
	require.NoError(t, err)
	assert.NotNil(t, service)
}

func TestNewEnv_failsWithAnUnusableConsulConfiguration(t *testing.T) {
	newMeshConsul(t, nil)
	t.Setenv("CONSUL_CACERT", "/nonexistent/ca.pem")

	env, err := NewEnv("carbonio.lab")
	require.Error(t, err)
	assert.Nil(t, env)
}

func TestNewOfflineEnv_refusesWhatNeedsTheCluster(t *testing.T) {
	t.Parallel()

	env := NewOfflineEnv("carbonio.lab")

	assert.Equal(t, "carbonio.lab", env.TrustDomain)
	assert.Equal(t, RenderedDir, env.RenderedDir)

	_, err := env.FetchJWT(Account{}, "spiffe://carbonio.lab/service/x")
	require.ErrorIs(t, err, errOffline)

	_, err = env.Login("jwt")
	require.ErrorIs(t, err, errOffline)

	valid, err := env.Valid("secret")
	require.ErrorIs(t, err, errOffline)
	assert.False(t, valid)

	require.ErrorIs(t, env.Logout("secret"), errOffline)

	isLogin, err := env.IsLogin("secret")
	require.ErrorIs(t, err, errOffline)
	assert.False(t, isLogin)

	service, err := env.Consul("token")
	require.ErrorIs(t, err, errOffline)
	assert.Nil(t, service)
}

func TestLookupUser(t *testing.T) {
	t.Parallel()

	root, err := lookupUser("root")
	require.NoError(t, err)
	assert.Equal(t, Account{UID: 0, GID: 0}, root)

	_, err = lookupUser("no-such-user-in-the-mesh-tests")
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRunAgentFetch(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "args")

	tests := []struct {
		name    string
		script  string
		want    string
		wantErr string
	}{
		{
			name:   "prints the token",
			script: `echo "$@" > ` + args + "\necho 'token(spiffe://carbonio.lab/service/x):'\necho '  header.payload.sig'",
			want:   "header.payload.sig",
		},
		{
			name:    "agent failing reports its error output",
			script:  "echo 'no identity issued' >&2\nexit 3",
			wantErr: "unable to fetch a JWT-SVID for spiffe://carbonio.lab/service/x: exit status 3: no identity issued",
		},
		{
			name:    "output without a token",
			script:  "echo 'nothing here'",
			wantErr: "JWT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary := envFakeScript(t, t.TempDir(), "spire-agent", tt.script)

			got, err := runAgentFetch(binary, nil, "spiffe://carbonio.lab/service/x")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	passed, err := os.ReadFile(args)
	require.NoError(t, err)
	assert.Equal(t, "api fetch jwt -audience "+spire.AudienceLogin+" -spiffeID spiffe://carbonio.lab/service/x -socketPath "+
		spire.AgentSocket+"\n", string(passed))
}

func TestFetchJWTAs_failsWithoutTheAgent(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat(spire.AgentBin); err == nil {
		t.Skip("a SPIRE agent is installed on this machine")
	}

	_, err := fetchJWTAs(Account{UID: os.Getuid(), GID: os.Getgid()}, "spiffe://carbonio.lab/service/x")
	require.ErrorContains(t, err, "unable to fetch a JWT-SVID for spiffe://carbonio.lab/service/x")
}

func TestSystemctl(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")

	envFakeScript(t, dir, systemctlBinary, `echo "$@" >> `+log+"\n"+`[ "$1" = fail ] && { echo 'unit not found' >&2; exit 5; }`+"\nexit 0")
	t.Setenv("PATH", dir)

	require.NoError(t, systemctl("restart", "carbonio-files"))

	err := systemctl("fail", "carbonio-files")
	require.ErrorContains(t, err, "exit status 5: unit not found")

	called, readErr := os.ReadFile(log)
	require.NoError(t, readErr)
	assert.Equal(t, "restart carbonio-files\nfail carbonio-files\n", string(called))
}

func TestAccountOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		user    user.User
		want    Account
		wantErr bool
	}{
		{"numeric ids", user.User{Uid: "1001", Gid: "1002"}, Account{UID: 1001, GID: 1002}, false},
		{"non numeric uid", user.User{Uid: "S-1-5-21", Gid: "1002"}, Account{}, true},
		{"non numeric gid", user.User{Uid: "1001", Gid: "staff"}, Account{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := accountOf(&tt.user)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantErr, err != nil)
		})
	}
}
