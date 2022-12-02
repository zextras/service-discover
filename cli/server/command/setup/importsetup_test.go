package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/carbonio"
	"bitbucket.org/zextras/service-discover/cli/lib/carbonio/mocks"
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	mocks3 "bitbucket.org/zextras/service-discover/cli/lib/exec/mocks"
	mocks4 "bitbucket.org/zextras/service-discover/cli/lib/systemd/mocks"
	"bitbucket.org/zextras/service-discover/cli/lib/test"
	mocks2 "bitbucket.org/zextras/service-discover/cli/server/command/setup/mocks"
	"bitbucket.org/zextras/service-discover/cli/server/config"
	"bytes"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"io/fs"
	"net"
	"os"
	native_exec "os/exec"
	"os/user"
	"syscall"
	"testing"
	"time"
)

type FakeFileStat struct {
	size int64
}

func (f FakeFileStat) Name() string {
	panic("implement me")
}

func (f FakeFileStat) Size() int64 {
	return f.size
}

func (f FakeFileStat) Mode() fs.FileMode {
	return fs.FileMode(0600)
}

func (f FakeFileStat) ModTime() time.Time {
	return time.Now()
}

func (f FakeFileStat) IsDir() bool {
	return false
}

func (f FakeFileStat) Sys() interface{} {
	panic("implement me")
}

func TestSetup_importSetup(t *testing.T) {
	t.Parallel()

	// Write the localconfig directly in a tmpFile, we will refactor this code anyway with something better
	zimbraLocalConfig, err := os.CreateTemp("/tmp", "testsetup_run")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(zimbraLocalConfig.Name(), test.GenerateLocalConfig(
		t,
		"mailbox-1.example.com",
		"ldap://mailbox-1.example.com:389",
		"ldap://mailbox-1.example.com:389",
		test.DefaultLdapUserDN,
		"pa$$word",
	), os.FileMode(0755)); err != nil {
		panic(err)
	}
	defer os.Remove(zimbraLocalConfig.Name())

	type setupOutput struct {
		consulConfigDir    string
		consulHome         string
		consulData         string
		consulFileConfig   string
		clusterCredentials string
		consulAclBootstrap string
		consulCertificate  string
		consulCAKeyFile    string
		mutableConfigFile  string
	}

	setup := func(testName string) (*setupOutput, func()) {
		consulConfigDir := test.GenerateRandomFolder(testName)
		consulHome := test.GenerateRandomFolder(testName)
		consulData := test.GenerateRandomFolder(testName)
		clusterFile := test.GenerateRandomFile(testName)
		consulAclBootstrap := test.GenerateRandomFile(testName)
		consulCertificate := test.GenerateRandomFile(testName)
		consulCAKeyFile := test.GenerateRandomFile(testName)
		clusterCredentialFile := test.GenerateRandomFile(testName)
		mutableConfigFile := test.GenerateRandomFile(testName)

		// Cleanup function
		return &setupOutput{
				consulConfigDir,
				consulHome,
				consulData,
				clusterFile.Name(),
				clusterCredentialFile.Name(),
				consulAclBootstrap.Name(),
				consulCertificate.Name(),
				consulCAKeyFile.Name(),
				mutableConfigFile.Name(),
			}, func() {
				if err := os.RemoveAll(consulConfigDir); err != nil {
					panic(err)
				}
				if err := os.RemoveAll(consulHome); err != nil {
					panic(err)
				}
				if err := os.RemoveAll(consulData); err != nil {
					panic(err)
				}
				if err := os.RemoveAll(clusterFile.Name()); err != nil {
					panic(err)
				}
				if err := os.RemoveAll(consulAclBootstrap.Name()); err != nil {
					panic(err)
				}
				if err := os.RemoveAll(consulCertificate.Name()); err != nil {
					panic(err)
				}
				if err := os.RemoveAll(consulCAKeyFile.Name()); err != nil {
					panic(err)
				}
				if err := os.RemoveAll(clusterCredentialFile.Name()); err != nil {
					panic(err)
				}
				if err := os.RemoveAll(mutableConfigFile.Name()); err != nil {
					panic(err)
				}
			}
	}

	t.Run("Cluster credentials is required", func(t *testing.T) {
		setupFiles, cleanup := setup("Test cluster credentials is required")
		defer cleanup()
		businessDep := new(mocks2.BusinessDependencies)
		setupNetwork(businessDep)
		setupLdapMock(businessDep)
		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   zimbraLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.clusterCredentials,
			MutableConfigFile: setupFiles.mutableConfigFile,
			BindAddress:       "127.0.0.1",
		}
		assert.NoError(t, os.Remove(setupFiles.clusterCredentials))
		_, err := s.importSetup(businessDep)
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"cannot find Cluster credential in %s, please copy the file from the existing server or upload it to LDAP",
				s.ClusterCredential,
			),
		)
	})

	t.Run("Wrong binding address", func(t *testing.T) {
		setupFiles, cleanup := setup("Wrong binding address")
		defer cleanup()
		businessDep := new(mocks2.BusinessDependencies)
		setupNetwork(businessDep)
		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   zimbraLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.clusterCredentials,
			MutableConfigFile: setupFiles.mutableConfigFile,
		}
		s.BindAddress = "wrong_one"
		_, err := s.importSetup(businessDep)
		assert.EqualError(
			t,
			err,
			"invalid binding address selected",
		)
	})

	t.Run("Wrong cluster credentials password", func(t *testing.T) {
		setupFiles, cleanup := setup("Wrong cluster credentials password")
		defer cleanup()
		businessDep := new(mocks2.BusinessDependencies)
		setupNetwork(businessDep)
		setupLdapMock(businessDep)
		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   zimbraLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.clusterCredentials,
			MutableConfigFile: setupFiles.mutableConfigFile,
			BindAddress:       "127.0.0.1",
			Password:          "not right one",
		}
		file, err := os.Create(setupFiles.clusterCredentials)
		assert.NoError(t, err)
		tarWriter, err := credentialsEncrypter.NewWriter(file, []byte("password"))
		assert.NoError(t, err)
		err = os.WriteFile(setupFiles.consulFileConfig, []byte("Test"), os.FileMode(0644))
		assert.NoError(t, err)
		consulFileConfig, err := os.Open(setupFiles.consulFileConfig)
		assert.NoError(t, err)
		stat, err := consulFileConfig.Stat()
		assert.NoError(t, err)

		assert.NoError(t, tarWriter.AddFile(consulFileConfig, stat, command.ConsulCA, config.ConsulHome))
		assert.NoError(t, tarWriter.Close())
		_, err = s.importSetup(businessDep)
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"unable to open %s: openpgp: incorrect key",
				setupFiles.clusterCredentials,
			),
		)
	})

	t.Run("Run with correct configuration and flags", func(t *testing.T) {
		setupFiles, cleanup := setup("Run with correct configuration and flags")
		defer cleanup()
		businessDep := new(mocks2.BusinessDependencies)
		setupNetwork(businessDep)
		setupBusinessDeps(businessDep)

		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   zimbraLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.clusterCredentials,
			MutableConfigFile: setupFiles.mutableConfigFile,
		}
		s.BindAddress = "127.0.0.1"
		s.Password = "password"
		clusterCredential, err := os.Create(setupFiles.clusterCredentials)
		assert.NoError(t, err)
		tarWriter, err := credentialsEncrypter.NewWriter(clusterCredential, []byte("password"))
		assert.NoError(t, err)

		assert.NoError(t, tarWriter.AddFile(
			bytes.NewBuffer([]byte("Test")),
			&FakeFileStat{size: 4},
			command.ConsulCA,
			setupFiles.consulHome+"/",
		))
		assert.NoError(t, tarWriter.AddFile(
			bytes.NewBuffer([]byte("Test")),
			&FakeFileStat{size: 4},
			command.ConsulCAKey,
			setupFiles.consulHome+"/",
		))
		assert.NoError(t, tarWriter.AddFile(
			bytes.NewBuffer([]byte("{\"blabla\": \"123\",\"SecretID\": \"c182a76b-d26f-92fb-de9b-2f828e8730bd\"}")),
			&FakeFileStat{size: 68},
			command.ConsulAclBootstrap,
			"/",
		))
		assert.NoError(t, tarWriter.AddFile(
			bytes.NewBuffer([]byte("random")),
			&FakeFileStat{size: 6},
			command.GossipKey,
			"/",
		))
		assert.NoError(t, tarWriter.Close())

		tlsCertCreateMock := new(mocks3.Cmd)
		tlsCertCreateMock.On("Output").Return([]byte("random"), nil)
		businessDep.On("CreateCommand",
			"/usr/bin/consul",
			"tls",
			"cert",
			"create",
			fmt.Sprintf("-days=%d", certificateExpiration),
			"-server",
		).Return(tlsCertCreateMock)

		tokenCreateMock := new(mocks3.Cmd)
		tokenCreateMock.On("Output").Return([]byte(`{
		  "SecretID": "secret-token-2"
		}`), nil)

		setTokenCmd := new(mocks3.Cmd)
		setTokenCmd.On("Output").Return(make([]byte, 0), nil)
		businessDep.On("CreateCommand",
			"/usr/bin/consul",
			"acl",
			"set-agent-token",
			"default",
			"secret-token-2",
		).Return(setTokenCmd)
		aclPolicyCreateMock := new(mocks3.Cmd)
		aclPolicyCreateMock.On("Output").Return([]byte("something"), nil)
		businessDep.On("CreateCommand",
			"/usr/bin/consul",
			"acl",
			"policy",
			"create",
			"-name",
			"server-mailbox-1-example-com",
			"-rules",
			`{
   "node":{
      "server-mailbox-1-example-com":{
         "policy":"write"
      }
   },
   "node_prefix":{
      "":{
         "policy":"read"
      }
   },
   "service_prefix":{
      "":{
         "policy":"write"
      }
   }
}`).Return(aclPolicyCreateMock, nil).
			On("CreateCommand",
				"/usr/bin/consul",
				"acl",
				"token",
				"create",
				"-policy-name",
				"server-mailbox-1-example-com",
				"-format",
				"json").
			Return(tokenCreateMock)

		var cleanups = make([]func(), 0)
		defer func() {
			for _, f := range cleanups {
				f()
			}
		}()
		setupLdapMock(businessDep)
		systemdUnitMock := new(mocks4.UnitManager)
		systemdUnitMock.On("StartUnit", "service-discover.service", "replace", mock.Anything).Return(
			0, nil,
		).Run(func(args mock.Arguments) {
			ch := args.Get(2).(chan<- string)

			cmd := native_exec.Command(
				"/usr/bin/consul",
				"agent",
				"-dev", // otherwise it takes up to 10 seconds to boostrap
				"-config-dir",
				s.ConsulConfigDir,
				"-server",
				"-bind",
				"127.0.0.1", // otherwise test address will be used
			)
			err := cmd.Start()
			if err != nil {
				panic(err)
			}

			cleanups = append(cleanups, func() {
				err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
				if err != nil {
					panic(err)
				}
			})
			time.Sleep(250 * time.Millisecond)
			ch <- "done"
		})
		systemdUnitMock.On("EnableUnitFiles", []string{"service-discover.service"}, false, false).Return(false, nil, nil)
		systemdUnitMock.On("Close").Return(nil)
		businessDep.On("SystemdUnitHandler").Return(systemdUnitMock, nil)

		_, err = s.importSetup(businessDep)
		assert.NoError(t, err)
	})
}

func setupLdapMock(businessDep *mocks2.BusinessDependencies) {
	ldapMockHandler := new(mocks.LdapHandler)
	ldapMockHandler.On("CheckServerAvailability", true).Return(nil)
	ldapMockHandler.
		On("AddService", "mailbox-1.example.com", carbonio.ServiceDiscoverServiceName).
		Return(nil)
	businessDep.On("LdapHandler", mock.Anything).Return(ldapMockHandler)
}

func setupBusinessDeps(businessDep *mocks2.BusinessDependencies) {
	businessDep.On(
		"LookupUser", "service-discover").Return(&user.User{
		Uid:      "1234",
		Gid:      "0",
		Username: "service-discover",
		Name:     "service-discover",
		HomeDir:  "/var/lib/service-discover",
	}, nil).On(
		"LookupGroup", "service-discover").Return(&user.Group{
		Gid:  "123456",
		Name: "service-discover",
	}, nil).On("Chown", mock.AnythingOfType("string"), 1234, 123456).Return(
		nil,
	).On("Chmod", mock.AnythingOfType("string"), os.FileMode(0600)).Return(
		nil,
	)
}
func setupNetwork(businessDep *mocks2.BusinessDependencies) {
	businessDep.On("NetInterfaces").Return([]net.Interface{
		{
			Index:        1, // Read GoDoc about net.Interface if you're puzzled by this
			MTU:          42,
			Name:         "lo",
			HardwareAddr: []byte("00:00:00:00:00:00"),
			Flags:        0,
		},
		{
			Index:        1, // Read GoDoc about net.Interface if you're puzzled by this
			MTU:          42,
			Name:         "eno0",
			HardwareAddr: []byte("78:bc:e6:2f:8a:d7"),
			Flags:        0,
		},
		{
			Index:        1,
			MTU:          42,
			Name:         "eno1",
			HardwareAddr: []byte("c6:f4:44:4f:9a:07"),
			Flags:        0,
		},
	}, nil).
		On("AddrResolver", mock.AnythingOfType("net.Interface")).Return([]net.Addr{
		&addrStub{ip: "127.0.0.1"},
		// We don't need any particular data here, just return something it is not the
		// bind address
	}, nil)

	businessDep.On("LookupIP", "mailbox-1.example.com").Return(
		[]net.IP{net.IPv4(1, 1, 1, 1)},
		nil,
	)
}
