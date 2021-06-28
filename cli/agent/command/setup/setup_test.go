package setup

import (
	"bitbucket.org/zextras/service-discover/cli/agent/command/setup/mocks"
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	mocks2 "bitbucket.org/zextras/service-discover/cli/lib/exec/mocks"
	mocks4 "bitbucket.org/zextras/service-discover/cli/lib/systemd/mocks"
	"bitbucket.org/zextras/service-discover/cli/lib/test"
	mocks5 "bitbucket.org/zextras/service-discover/cli/lib/zimbra/mocks"
	"bytes"
	"fmt"
	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"html/template"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"testing"
)

type addrStub struct {
	ip string
}

func (a *addrStub) Network() string {
	return "tcp"
}

func (a *addrStub) String() string {
	return a.ip
}

func TestSetup_preRun(t *testing.T) {
	t.Parallel()

	t.Run("Should fail if consul is not present", func(t *testing.T) {
		expectedErrorOutput := "awesome-error"
		missingConsul := new(mocks2.Cmd)
		missingConsul.On("Run").Return(errors.New(expectedErrorOutput))
		mockedDep := new(mocks.BusinessDependencies)
		mockedDep.On("CreateCommand", command.ConsulBin, "version").Return(missingConsul)
		assert.EqualError(t,
			preRun("", mockedDep),
			fmt.Sprintf("unable to execute consul binary: %s", expectedErrorOutput),
		)
	})

	t.Run("Should fail if command is not run as root", func(t *testing.T) {
		missingConsul := new(mocks2.Cmd)
		missingConsul.On("Run").Return(nil)
		mockedDep := new(mocks.BusinessDependencies)
		mockedDep.
			On("CreateCommand", command.ConsulBin, "version").
			Return(missingConsul).
			On("GetuidSyscall").
			Return(42)
		assert.EqualError(t,
			preRun("", mockedDep),
			"this command must be executed as root",
		)
	})

	t.Run("Should fail if cluster credential file is missing", func(t *testing.T) {
		clusterCredentialPath := "bogus-value"
		missingConsul := new(mocks2.Cmd)
		missingConsul.On("Run").Return(nil)
		mockedDep := new(mocks.BusinessDependencies)
		mockedDep.
			On("CreateCommand", command.ConsulBin, "version").
			Return(missingConsul).
			On("GetuidSyscall").
			Return(0)
		assert.EqualError(t,
			preRun(clusterCredentialPath, mockedDep),
			fmt.Sprintf(
				"cannot find Cluster credential in %s, please copy the file from the existing server",
				clusterCredentialPath,
			),
		)
	})

	t.Run("Should pass", func(t *testing.T) {
		clusterCredential := test.GenerateRandomFile("Should pass")
		defer os.Remove(clusterCredential.Name())
		missingConsul := new(mocks2.Cmd)
		missingConsul.On("Run").Return(nil)
		mockedDep := new(mocks.BusinessDependencies)
		mockedDep.
			On("CreateCommand", command.ConsulBin, "version").
			Return(missingConsul).
			On("GetuidSyscall").
			Return(0)
		assert.NoError(t, preRun(clusterCredential.Name(), mockedDep))
	})
}

func TestSetup_setup(t *testing.T) {
	t.Parallel()

	t.Run("Should fail when invalid binding address is selected", func(t *testing.T) {
		selectedInterface := net.Interface{
			Name: "en1",
		}
		mockDep := new(mocks.BusinessDependencies)
		mockDep.On("NetInterfaces").Return([]net.Interface{
			selectedInterface,
			net.Interface{
				Name: "lo",
			},
		}, nil).
			On("AddrResolver", selectedInterface).Return([]net.Addr{
			&addrStub{ip: "192.168.1.1"},
		}, nil)
		s := &Setup{
			BindAddress: "invalid-binding-address",
		}
		_, err := s.setup(mockDep)
		assert.EqualError(t, err, "invalid binding address selected")
	})

	t.Run("Should fail when cluster credentials are missing", func(t *testing.T) {
		nonExistingFilename := "you-wish-there-is-a-file-here"
		selectedInterface := net.Interface{
			Name: "en1",
		}
		mockDep := new(mocks.BusinessDependencies)
		mockDep.On("NetInterfaces").Return([]net.Interface{
			selectedInterface,
			net.Interface{
				Name: "lo",
			},
		}, nil).
			On("AddrResolver", selectedInterface).Return([]net.Addr{
			&addrStub{ip: "192.168.1.1"},
		}, nil)
		s := &Setup{
			ClusterCredential: "you-wish-there-is-a-file-here",
			BindAddress:       "192.168.1.1",
		}
		_, err := s.setup(mockDep)
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"unable to open %s: cannot find Cluster credential in %s, please copy the file from the existing server",
				nonExistingFilename,
				nonExistingFilename,
			),
		)
	})

	t.Run("Should fail when a wrong password is set", func(t *testing.T) {
		clusterCredential := test.GenerateRandomFile("Should fail when a wrong password is set")
		defer os.Remove(clusterCredential.Name())
		writer, err := credentialsEncrypter.NewWriter(clusterCredential, []byte("password"))
		assert.NoError(t, err)
		assert.NoError(t, writer.Close())
		selectedInterface := net.Interface{
			Name: "en1",
		}
		mockDep := new(mocks.BusinessDependencies)
		mockDep.On("NetInterfaces").Return([]net.Interface{
			selectedInterface,
			net.Interface{
				Name: "lo",
			},
		}, nil).
			On("AddrResolver", selectedInterface).Return([]net.Addr{
			&addrStub{ip: "192.168.1.1"},
		}, nil)
		s := &Setup{
			ClusterCredential: clusterCredential.Name(),
			BindAddress:       "192.168.1.1",
			Password:          "wrong-password",
		}
		_, err = s.setup(mockDep)
		assert.EqualError(
			t,
			err,
			"openpgp: incorrect key",
		)
	})

	t.Run("Should fail to create TLS certificate if CA is not present", func(t *testing.T) {
		clusterCredential := test.GenerateRandomFile("Should fail to create TLS certificate if CA is not present")
		defer os.Remove(clusterCredential.Name())
		consulHome := test.GenerateRandomFolder("Should fail to create TLS certificate if CA is not present")
		defer os.RemoveAll(consulHome)
		writer, err := credentialsEncrypter.NewWriter(clusterCredential, []byte("password"))
		assert.NoError(t, err)
		assert.NoError(t, writer.Close())
		selectedInterface := net.Interface{
			Name: "en1",
		}
		mockDep := new(mocks.BusinessDependencies)
		mockDep.On("NetInterfaces").Return([]net.Interface{
			selectedInterface,
			net.Interface{
				Name: "lo",
			},
		}, nil).
			On("AddrResolver", selectedInterface).Return([]net.Addr{
			&addrStub{ip: "192.168.1.1"},
		}, nil)
		s := &Setup{
			ClusterCredential: clusterCredential.Name(),
			BindAddress:       "192.168.1.1",
			Password:          "password",
			ConsulHome:        consulHome,
		}
		_, err = s.setup(mockDep)
		expectedCaPath, _ := filepath.Rel("/", path.Join(consulHome, command.ConsulCA))
		expectedCaKeyPath, _ := filepath.Rel("/", path.Join(consulHome, command.ConsulCAKey))
		missingFiles := " " + expectedCaPath + " " + expectedCaKeyPath + " " + command.GossipKey + " " + command.ConsulAclBootstrap
		assert.EqualError(
			t,
			err,
			fmt.Sprintf("not all files where found in the archive:%s", missingFiles),
		)
	})

	t.Run("Should fail when systemd enabling service-discover unit returns an error", func(t *testing.T) {
		testName := "Should fail when systemd enabling service-discover unit returns an error"
		errorCause := "this is the error"
		unitName := "service-discover.service"
		mutableConfiguration := test.GenerateRandomFile(testName)
		defer os.Remove(mutableConfiguration.Name())
		localConfig := test.GenerateRandomFile(testName)
		defer os.Remove(localConfig.Name())
		assert.NoError(t, ioutil.WriteFile(localConfig.Name(), test.GenerateLocalConfig(
			t,
			"mailbox-1.example.com",
			"ldap://mailbox-1.example.com:389",
			"ldap://mailbox-1.example.com:389",
			test.DefaultLdapUserDN,
			"pa$$word",
		), 0644))
		clusterCredential := test.GenerateRandomFile(testName)
		defer os.Remove(clusterCredential.Name())
		mutableConfig := test.GenerateRandomFile(testName)
		defer os.Remove(mutableConfig.Name())
		consulHome := test.GenerateRandomFolder(testName)
		defer os.RemoveAll(consulHome)
		writer, err := credentialsEncrypter.NewWriter(clusterCredential, []byte("password"))
		assert.NoError(t, err)
		dumbCaContent, caStat := test.CreateDumbFile([]byte("Test"), command.ConsulCA)
		assert.NoError(t, writer.AddFile(dumbCaContent, caStat, command.ConsulCA, consulHome+"/"))
		dumbGossipKeyContent, gossipStat := test.CreateDumbFile([]byte("Gossip-test"), command.GossipKey)
		assert.NoError(t, writer.AddFile(dumbGossipKeyContent, gossipStat, command.GossipKey, "/"))
		dumbCaKeyContent, caKeyStat := test.CreateDumbFile([]byte("Gossip-test"), command.ConsulCAKey)
		assert.NoError(t, writer.AddFile(dumbCaKeyContent, caKeyStat, command.ConsulCAKey, consulHome+"/"))
		dumbAclContent, aclStat := test.CreateDumbFile([]byte(`{
  "SecretID": "secret-token"
}`), command.ConsulAclBootstrap)
		assert.NoError(t, writer.AddFile(dumbAclContent, aclStat, command.ConsulAclBootstrap, "/"))
		assert.NoError(t, writer.Close())
		mockDep := new(mocks.BusinessDependencies)
		certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
		tlsCaCreateCmd := new(mocks2.Cmd)
		tlsCaCreateCmd.On("Output").Return(make([]byte, 0), nil)
		aclCreateCmd := new(mocks2.Cmd)
		aclCreateCmd.On("Output").Return(make([]byte, 0), nil)
		tokenCreateCmd := new(mocks2.Cmd)
		tokenCreateCmd.On("Output").Return([]byte(`{
		  "SecretID": "secret-token-2"
		}`), nil)
		setTokenCmd := new(mocks2.Cmd)
		setTokenCmd.On("Output").Return(make([]byte, 0), nil)
		selectedInterface := net.Interface{
			Name: "en1",
		}
		aclTemplateData := struct {
			ZimbraHostname string
		}{ZimbraHostname: "agent-mailbox-1-example-com"}
		aclTemplate := template.Must(template.New("acl").Parse(command.AclPolicyTemplateText))
		aclRenderOut := bytes.Buffer{}
		assert.NoError(t, aclTemplate.Execute(&aclRenderOut, aclTemplateData))
		aclRenderBs, err := ioutil.ReadAll(&aclRenderOut)
		assert.NoError(t, err)
		mockDep.On("LookupIP", "mailbox-1.example.com").Return([]net.IP{net.IPv4(1, 1, 1, 1)}, nil)
		mockDep.On("CreateCommand",
			"/usr/bin/consul",
			"tls",
			"cert",
			"create",
			certificateDaysFlag,
			"-ca",
			mock.AnythingOfType("string"),
			"-key",
			mock.AnythingOfType("string"),
			"-client",
		).
			Return(tlsCaCreateCmd).
			On("CreateCommand",
				"/usr/bin/consul",
				"acl",
				"policy",
				"create",
				"-name",
				"agent-mailbox-1-example-com",
				"-rules",
				string(aclRenderBs),
			).
			Return(aclCreateCmd).
			On("CreateCommand",
				"/usr/bin/consul",
				"acl",
				"token",
				"create",
				"-policy-name",
				"agent-mailbox-1-example-com",
				"-format",
				"json",
			).
			Return(tokenCreateCmd).
			On("CreateCommand",
				"/usr/bin/consul",
				"acl",
				"set-agent-token",
				"default",
				"secret-token-2",
			).Return(setTokenCmd).
			On("NetInterfaces").Return([]net.Interface{
			selectedInterface,
			net.Interface{
				Name: "lo",
			},
		}, nil).
			On("AddrResolver", selectedInterface).Return([]net.Addr{
			&addrStub{ip: "192.168.1.1"},
		}, nil)
		unitManager := new(mocks4.UnitManager)
		unitManager.On("EnableUnitFiles", []string{unitName}, false, false).
			Return(false, make([]dbus.EnableUnitFileChange, 0), errors.New(errorCause)).
			On("Close").
			Return(nil).
			On("StartUnit", "service-discover.service", "replace", mock.Anything).
			Return(0, nil).
			Run(func(args mock.Arguments) {
				ch := args.Get(2).(chan<- string)
				ch <- "done"
			})
		mockLdapHandler := new(mocks5.LdapHandler)
		mockLdapHandler.On("CheckServerAvailability", true).
			Return(nil)
		mockDep.
			On("SystemdUnitHandler").
			Return(unitManager, nil).
			On("LdapHandler", mock.Anything).
			Return(mockLdapHandler)
		s := &Setup{
			ConsulHome:        consulHome,
			LocalConfigPath:   localConfig.Name(),
			ConsulFileConfig:  mutableConfiguration.Name(),
			ClusterCredential: clusterCredential.Name(),
			MutableConfigFile: mutableConfig.Name(),
			Password:          "password",
			BindAddress:       "192.168.1.1",
		}
		_, err = s.setup(mockDep)
		assert.EqualError(t, err, fmt.Sprintf("unable to enable %s unit: %s", unitName, errorCause))
	})

	t.Run("Should properly run without errors", func(t *testing.T) {
		testName := "Should properly run without errors"
		mutableConfiguration := test.GenerateRandomFile(testName)
		defer os.Remove(mutableConfiguration.Name())
		localConfig := test.GenerateRandomFile(testName)
		defer os.Remove(localConfig.Name())
		assert.NoError(t, ioutil.WriteFile(localConfig.Name(), test.GenerateLocalConfig(
			t,
			"mailbox-1.example.com",
			"ldap://mailbox-1.example.com:389",
			"ldap://mailbox-1.example.com:389",
			test.DefaultLdapUserDN,
			"pa$$word",
		), 0644))
		clusterCredential := test.GenerateRandomFile(testName)
		defer os.Remove(clusterCredential.Name())
		mutableConfig := test.GenerateRandomFile(testName)
		defer os.Remove(mutableConfig.Name())
		consulHome := test.GenerateRandomFolder(testName)
		defer os.RemoveAll(consulHome)
		writer, err := credentialsEncrypter.NewWriter(clusterCredential, []byte("password"))
		assert.NoError(t, err)
		dumbCaContent, caStat := test.CreateDumbFile([]byte("Test"), command.ConsulCA)
		assert.NoError(t, writer.AddFile(dumbCaContent, caStat, command.ConsulCA, consulHome+"/"))
		dumbGossipKeyContent, gossipStat := test.CreateDumbFile([]byte("Gossip-test"), command.GossipKey)
		assert.NoError(t, writer.AddFile(dumbGossipKeyContent, gossipStat, command.GossipKey, "/"))
		dumbAclContent, aclStat := test.CreateDumbFile([]byte(`{
  "SecretID": "secret-token"
}`), command.ConsulAclBootstrap)
		assert.NoError(t, writer.AddFile(dumbAclContent, aclStat, command.ConsulAclBootstrap, "/"))
		dumbCaKeyContent, caKeyStat := test.CreateDumbFile([]byte("Gossip-test"), command.GossipKey)
		assert.NoError(t, writer.AddFile(dumbCaKeyContent, caKeyStat, command.ConsulCAKey, consulHome+"/"))
		assert.NoError(t, writer.Close())
		mockDep := new(mocks.BusinessDependencies)
		certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
		tlsCaCreateCmd := new(mocks2.Cmd)
		tlsCaCreateCmd.On("Output").Return(make([]byte, 0), nil)
		aclCreateCmd := new(mocks2.Cmd)
		aclCreateCmd.On("Output").Return(make([]byte, 0), nil)
		tokenCreateCmd := new(mocks2.Cmd)
		tokenCreateCmd.On("Output").Return([]byte(`{
		  "SecretID": "secret-token-2"
		}`), nil)
		setTokenCmd := new(mocks2.Cmd)
		setTokenCmd.On("Output").Return(make([]byte, 0), nil)
		selectedInterface := net.Interface{
			Name: "en1",
		}
		aclTemplateData := struct {
			ZimbraHostname string
		}{ZimbraHostname: "agent-mailbox-1-example-com"}
		aclTemplate := template.Must(template.New("acl").Parse(command.AclPolicyTemplateText))
		aclRenderOut := bytes.Buffer{}
		assert.NoError(t, aclTemplate.Execute(&aclRenderOut, aclTemplateData))
		aclRenderBs, err := ioutil.ReadAll(&aclRenderOut)
		assert.NoError(t, err)
		mockDep.On("LookupIP", "mailbox-1.example.com").Return([]net.IP{net.IPv4(1, 1, 1, 1)}, nil)
		mockDep.On("CreateCommand",
			"/usr/bin/consul",
			"tls",
			"cert",
			"create",
			certificateDaysFlag,
			"-ca",
			mock.AnythingOfType("string"),
			"-key",
			mock.AnythingOfType("string"),
			"-client",
		).
			Return(tlsCaCreateCmd).
			On("CreateCommand",
				"/usr/bin/consul",
				"acl",
				"policy",
				"create",
				"-name",
				"agent-mailbox-1-example-com",
				"-rules",
				string(aclRenderBs),
			).
			Return(aclCreateCmd).
			On("CreateCommand",
				"/usr/bin/consul",
				"acl",
				"token",
				"create",
				"-policy-name",
				"agent-mailbox-1-example-com",
				"-format",
				"json",
			).
			Return(tokenCreateCmd).
			On("CreateCommand",
				"/usr/bin/consul",
				"acl",
				"set-agent-token",
				"default",
				"secret-token-2",
			).Return(setTokenCmd).
			On("NetInterfaces").Return([]net.Interface{
			selectedInterface,
			net.Interface{
				Name: "lo",
			},
		}, nil).
			On("AddrResolver", selectedInterface).Return([]net.Addr{
			&addrStub{ip: "192.168.1.1"},
		}, nil)
		unitManager := new(mocks4.UnitManager)
		unitManager.On("EnableUnitFiles", []string{"service-discover.service"}, false, false).
			Return(false, make([]dbus.EnableUnitFileChange, 0), nil).
			On("Close").
			Return(nil).
			On("StartUnit", "service-discover.service", "replace", mock.Anything).
			Return(0, nil).
			Run(func(args mock.Arguments) {
				ch := args.Get(2).(chan<- string)
				ch <- "done"
			})
		mockDep.On("SystemdUnitHandler").Return(unitManager, nil)
		mockLdapHandler := new(mocks5.LdapHandler)
		mockLdapHandler.On("CheckServerAvailability", true).
			Return(nil)
		mockDep.
			On("SystemdUnitHandler").
			Return(unitManager, nil).
			On("LdapHandler", mock.Anything).
			Return(mockLdapHandler)
		s := &Setup{
			LocalConfigPath:   localConfig.Name(),
			ConsulFileConfig:  mutableConfiguration.Name(),
			ClusterCredential: clusterCredential.Name(),
			MutableConfigFile: mutableConfig.Name(),
			BindAddress:       "192.168.1.1",
			Password:          "password",
			ConsulHome:        consulHome,
		}
		_, err = s.setup(mockDep)
		assert.NoError(t, err)
		mutableConfigContent, err := ioutil.ReadFile(mutableConfig.Name())
		assert.NoError(t, err)
		assert.Equal(t, `{
  "bind_addr": "192.168.1.1"
}`, string(mutableConfigContent))
	})
}
