package command

import (
	"fmt"
	"github.com/Zextras/service-discover/cli/lib/credentialsEncrypter"
	"github.com/Zextras/service-discover/cli/lib/term"
	"github.com/Zextras/service-discover/cli/lib/test"
	"github.com/stretchr/testify/assert"
	"io"
	"os"
	"testing"
)

type fakeNotTerminal struct {
	fakeTerminal
}

func (t fakeNotTerminal) Get(writer io.Writer) (term.Terminal, error) {
	return &fakeNotTerminal{t.fakeTerminal}, nil
}

func (t fakeNotTerminal) ReadPassword(prompt string) (string, error) {
	println("Not a terminal!")
	return "", term.NotATerminalError(1)
}

func (t fakeNotTerminal) ReadLine() (string, error) {
	return t.Password, nil
}

type fakeTerminal struct {
	Password string
}

func (t fakeTerminal) Get(writer io.Writer) (term.Terminal, error) {
	return &fakeTerminal{t.Password}, nil
}

func (t fakeTerminal) Write(p []byte) (n int, err error) {
	return 0, nil
}

func (t fakeTerminal) Close() error {
	return nil
}

func (t fakeTerminal) WriteString(s string) (n int, err error) {
	return 0, nil
}

func (t fakeTerminal) ReadPassword(prompt string) (string, error) {
	return t.Password, nil
}

func (t fakeTerminal) ReadLine() (string, error) {
	return "", nil
}

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
			name: "Bootstrap Token with --Password should return token",
			setup: BootstrapToken{
				Command:                       *cmd,
				writer:                        os.Stdout,
				agentName:                     "myAgent",
				Setup:                         false,
				Password:                      password,
				clusterCredentialFileLocation: clusterCredentialsFile.Name()},
			want: token,
		},
		{
			name: "Bootstrap Token should not fail when providing Password through terminal",
			setup: BootstrapToken{
				Command:                       *cmd,
				writer:                        os.Stdout,
				agentName:                     "myAgent",
				Setup:                         false,
				clusterCredentialFileLocation: clusterCredentialsFile.Name(),
				termUiProvider:                &fakeTerminal{Password: password},
			},
			want: token,
		},
		{
			name: "Bootstrap Token should not fail when providing Password through terminal",
			setup: BootstrapToken{
				Command:                       *cmd,
				writer:                        os.Stdout,
				agentName:                     "myAgent",
				Setup:                         false,
				clusterCredentialFileLocation: clusterCredentialsFile.Name(),
				termUiProvider:                &fakeTerminal{Password: password},
			},
			want: token,
		},
		{
			name: "Bootstrap Token should not fail if not a terminal",
			setup: BootstrapToken{
				Command:                       *cmd,
				writer:                        os.Stdout,
				agentName:                     "myAgent",
				Setup:                         true,
				clusterCredentialFileLocation: clusterCredentialsFile.Name(),
				termUiProvider:                &fakeNotTerminal{fakeTerminal{Password: password}},
			},
			want: token,
		},
		// Failures
		{
			name: "Bootstrap Token with --Password and default credentials should fail if not found",
			setup: BootstrapToken{
				Command:                       *cmd,
				writer:                        os.Stdout,
				agentName:                     "myAgent",
				Setup:                         false,
				Password:                      password,
				clusterCredentialFileLocation: "/my/non/existing/file.tar.gpg",
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Equal(t, "unable to open /my/non/existing/file.tar.gpg: cannot find Cluster credential in /my/non/existing/file.tar.gpg, please copy the file from the existing server or upload it to LDAP", err.Error())
			},
		},
		{
			name: "Bootstrap Token with wrong --Password should fail",
			setup: BootstrapToken{
				Command:                       *cmd,
				writer:                        os.Stdout,
				agentName:                     "myAgent",
				Setup:                         false,
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
