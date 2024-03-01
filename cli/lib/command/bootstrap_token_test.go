package command

import (
	"fmt"
	"github.com/Zextras/service-discover/cli/lib/credentialsEncrypter"
	"github.com/Zextras/service-discover/cli/lib/test"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestBootstrapToken_(t *testing.T) {
	cmd := NewCommand(
		"TestApp",
		"1.0",
	)
	password := "testPassword"
	token := "testToken"

	clusterCredentialsFile := test.GenerateRandomFile("test-bootstrap-token-cluster-credentials")
	defer os.RemoveAll(clusterCredentialsFile.Name())

	writer, _ := credentialsEncrypter.NewWriter(clusterCredentialsFile, []byte(password))

	dumbAclContent, aclStat := test.CreateDumbFile([]byte(fmt.Sprintf(`{
		"SecretID":"%s"
	}`, token)), ConsulAclBootstrap)
	assert.NoError(t, writer.AddFile(dumbAclContent, aclStat, ConsulAclBootstrap, "/"))
	assert.NoError(t, writer.Flush())
	assert.NoError(t, writer.Close())

	tests := []struct {
		name    string
		setup   BootstrapToken
		want    string
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "Bootstrap Token with --password should return token",
			setup: BootstrapToken{Command: *cmd, writer: os.Stdout, agentName: "myAgent", Setup: false,
				Password:                      password,
				clusterCredentialFileLocation: clusterCredentialsFile.Name()},
			want: token,
		},
		{
			name: "Bootstrap Token with --password and default credentials should fail if not found",
			setup: BootstrapToken{Command: *cmd, writer: os.Stdout,
				agentName: "myAgent", Setup: false, Password: password, clusterCredentialFileLocation: "/my/non/existing/file.tar.gpg"},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Equal(t, "unable to open /my/non/existing/file.tar.gpg: cannot find Cluster credential in /my/non/existing/file.tar.gpg, please copy the file from the existing server or upload it to LDAP", err.Error())
			},
		},
		{
			name: "Bootstrap Token without --password should fail if not terminal",
			setup: BootstrapToken{Command: *cmd, writer: os.Stdout, agentName: "myAgent", Setup: false,
				clusterCredentialFileLocation: clusterCredentialsFile.Name()},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Equal(t, "the provided file descriptor (0) is not a terminal", err.Error())
			},
		},
		{
			name: "Bootstrap Token with wrong --password should fail",
			setup: BootstrapToken{
				Command: *cmd, writer: os.Stdout, agentName: "myAgent", Setup: false,
				Password:                      "wrongPassword",
				clusterCredentialFileLocation: clusterCredentialsFile.Name(),
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Equal(t, "openpgp: incorrect key", err.Error())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.setup.ReadToken()
			if err != nil && tt.wantErr != nil {
				tt.wantErr(t, err)
			} else {
				t.Logf("Got error %v", err)
				assert.Equalf(t, tt.want, got, "ReadToken()")
			}
		})
	}
}
