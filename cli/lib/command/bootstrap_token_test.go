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

	t.Logf("Wrote ACL test content to: %v", clusterCredentialsFile.Name())

	tests := []struct {
		name    string
		setup   BootstrapToken
		want    string
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "Bootstrap Token with --password should return token",
			setup: BootstrapToken{Command: *cmd, writer: os.Stdout, agentName: "myAgent", Setup: false,
				Password:             password,
				bootstrapTokenConfig: BootstrapTokenConfig{ClusterCredentialLocation: clusterCredentialsFile.Name()}},
			want: token,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.setup.ReadToken()
			t.Logf("Got error %v", err)
			assert.Equalf(t, tt.want, got, "ReadToken()")
		})
	}
}
