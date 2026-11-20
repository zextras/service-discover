// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/test"
)

func TestBootstrapToken_Token(t *testing.T) {
	const (
		password = "tokenPassword"
		fromFile = "token-from-the-credentials"
	)

	credentials := test.GenerateRandomFile("test-bootstrap-token-env-credentials")
	defer os.RemoveAll(credentials.Name())

	writer, err := encrypter.NewWriter(credentials, []byte(password))
	require.NoError(t, err)

	content, stat := test.CreateDumbFile([]byte(fmt.Sprintf(`{"SecretID":"%s"}`, fromFile)), ConsulACLBootstrap)
	require.NoError(t, writer.AddFile(content, stat, ConsulACLBootstrap, "/"))
	require.NoError(t, writer.Flush())
	require.NoError(t, writer.Close())

	tests := []struct {
		name    string
		env     *string
		want    string
		wantErr bool
		file    string
	}{
		{name: "the environment wins over the credentials", env: ptr("token-from-env"), want: "token-from-env", file: "/missing"},
		{name: "an empty environment reads the credentials", env: ptr(""), want: fromFile, file: credentials.Name()},
		{name: "no environment reads the credentials", want: fromFile, file: credentials.Name()},
		{name: "unreadable credentials fail", wantErr: true, file: "/my/non/existing/file.tar.gpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != nil {
				t.Setenv(SetupConsulToken, *tt.env)
			} else {
				unsetForTest(t, SetupConsulToken)
			}

			bootstrap := &BootstrapToken{Password: password, clusterCredentialFileLocation: tt.file}

			got, err := bootstrap.Token()
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func ptr(value string) *string { return &value }

// unsetForTest removes the variable for the test and restores it afterwards.
func unsetForTest(t *testing.T, key string) {
	t.Helper()

	previous, present := os.LookupEnv(key)

	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(key, previous)
		}
	})
}

func TestBootstrapToken_StoredToken(t *testing.T) {
	const (
		password = "storedPassword"
		fromFile = "token-from-the-stored-password"
	)

	credentials := test.GenerateRandomFile("test-bootstrap-token-stored-credentials")
	defer os.RemoveAll(credentials.Name())

	writer, err := encrypter.NewWriter(credentials, []byte(password))
	require.NoError(t, err)

	content, stat := test.CreateDumbFile([]byte(fmt.Sprintf(`{"SecretID":"%s"}`, fromFile)), ConsulACLBootstrap)
	require.NoError(t, writer.AddFile(content, stat, ConsulACLBootstrap, "/"))
	require.NoError(t, writer.Flush())
	require.NoError(t, writer.Close())

	passwordFile := func(content string) string {
		path := filepath.Join(t.TempDir(), "password")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o400))

		return path
	}

	t.Run("decrypts the credentials with the stored password, asking nothing", func(t *testing.T) {
		unsetForTest(t, SetupConsulToken)

		bootstrap := &BootstrapToken{clusterCredentialFileLocation: credentials.Name()}

		got, err := bootstrap.StoredToken(passwordFile(password))
		require.NoError(t, err)
		assert.Equal(t, fromFile, got)
	})

	t.Run("the environment wins", func(t *testing.T) {
		t.Setenv(SetupConsulToken, "token-from-env")

		got, err := (&BootstrapToken{}).StoredToken("/missing")
		require.NoError(t, err)
		assert.Equal(t, "token-from-env", got)
	})

	t.Run("no stored password", func(t *testing.T) {
		unsetForTest(t, SetupConsulToken)

		bootstrap := &BootstrapToken{clusterCredentialFileLocation: credentials.Name()}

		_, err := bootstrap.StoredToken(filepath.Join(t.TempDir(), "absent"))
		require.ErrorIs(t, err, ErrNoStoredPassword)

		_, err = bootstrap.StoredToken(passwordFile("\n"))
		require.ErrorIs(t, err, ErrNoStoredPassword)
	})

	t.Run("a wrong password fails", func(t *testing.T) {
		unsetForTest(t, SetupConsulToken)

		bootstrap := &BootstrapToken{clusterCredentialFileLocation: credentials.Name()}

		_, err := bootstrap.StoredToken(passwordFile("wrong"))
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrNoStoredPassword)
	})
}
