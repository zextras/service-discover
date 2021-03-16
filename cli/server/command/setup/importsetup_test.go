package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	"bitbucket.org/zextras/service-discover/cli/lib/test"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	mocks2 "bitbucket.org/zextras/service-discover/cli/server/command/setup/mocks"
	"bitbucket.org/zextras/service-discover/cli/server/config"
	mocks3 "bitbucket.org/zextras/service-discover/cli/server/exec/mocks"
	"bitbucket.org/zextras/service-discover/cli/server/mocks"
	mocks4 "bitbucket.org/zextras/service-discover/cli/server/systemd/mocks"
	"fmt"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"io"
	"io/ioutil"
	"net"
	"os"
	"testing"
)

func TestSetup_importSetup(t *testing.T) {
	t.Parallel()

	// Write the localconfig directly in a tmpFile, we will refactor this code anyway with something better
	zimbraLocalConfig, err := ioutil.TempFile("/tmp", "testsetup_run")
	if err != nil {
		panic(err)
	}
	xmlLocalConfig := `<?xml version="1.0" encoding="UTF-8"?>
<localconfig>
<key name="zimbra_server_hostname">
  <value>mailbox-1.example.com</value>
</key>
<key name="ldap_master_url">
  <value>ldap://mailbox-1.example.com:389</value>
</key>
<key name="ldap_url">
  <value>ldap://mailbox-1.example.com:389</value>
</key>
<key name="zimbra_ldap_userdn">
  <value>uid=zimbra,cn=admins,cn=zimbra</value>
</key>
<key name="zimbra_ldap_password">
  <value>pa$$word</value>
</key>
</localconfig>`
	if err := ioutil.WriteFile(zimbraLocalConfig.Name(), []byte(xmlLocalConfig), os.FileMode(0755)); err != nil {
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
				err := os.RemoveAll(consulConfigDir)
				err = os.RemoveAll(consulHome)
				err = os.RemoveAll(consulData)
				err = os.RemoveAll(clusterFile.Name())
				err = os.RemoveAll(consulAclBootstrap.Name())
				err = os.RemoveAll(consulCertificate.Name())
				err = os.RemoveAll(consulCAKeyFile.Name())
				err = os.RemoveAll(clusterCredentialFile.Name())
				err = os.RemoveAll(mutableConfigFile.Name())
				if err != nil { // Any error
					panic(err)
				}
			}
	}

	t.Run("Cluster credentials is required", func(t *testing.T) {
		setupFiles, cleanup := setup("Test cluster credentials is required")
		defer cleanup()
		businessDep := new(mocks2.BusinessDependencies)
		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   zimbraLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.clusterCredentials,
			MutableConfigFile: setupFiles.mutableConfigFile,
		}
		assert.NoError(t, os.Remove(setupFiles.clusterCredentials))
		_, err := s.importSetup(businessDep)
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"cannot find Cluster credential in %s, please copy the file from the existing server",
				s.ClusterCredential,
			),
		)
	})

	t.Run("Wrong binding address", func(t *testing.T) {
		setupFiles, cleanup := setup("Wrong binding address")
		defer cleanup()
		businessDep := new(mocks2.BusinessDependencies)
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

	t.Run("Missing file in cluster credential", func(t *testing.T) {
		setupFiles, cleanup := setup("Missing file in cluster credential")
		defer cleanup()
		businessDep := new(mocks2.BusinessDependencies)
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
		err = ioutil.WriteFile(setupFiles.consulFileConfig, []byte("Test"), os.FileMode(0644))
		assert.NoError(t, err)
		consulFileConfig, err := os.Open(setupFiles.consulFileConfig)
		assert.NoError(t, err)
		stat, err := consulFileConfig.Stat()
		assert.NoError(t, err)

		assert.NoError(t, tarWriter.AddFile(consulFileConfig, stat, ConsulCAFile, config.ConsulHome))
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
		err = ioutil.WriteFile(setupFiles.consulFileConfig, []byte("Test"), os.FileMode(0644))
		assert.NoError(t, err)
		err = ioutil.WriteFile(setupFiles.consulCAKeyFile, []byte("Test"), os.FileMode(0644))
		assert.NoError(t, err)
		err = ioutil.WriteFile(setupFiles.consulCertificate, []byte("Test"), os.FileMode(0644))
		assert.NoError(t, err)
		err = ioutil.WriteFile(setupFiles.consulAclBootstrap, []byte("Test"), os.FileMode(0644))
		assert.NoError(t, err)
		consulFileConfig, err := os.Open(setupFiles.consulFileConfig)
		assert.NoError(t, err)
		consulCAKey, err := os.Open(setupFiles.consulCAKeyFile)
		assert.NoError(t, err)
		consulCertificate, err := os.Open(setupFiles.consulCertificate)
		assert.NoError(t, err)
		consulAclBootstrap, err := os.Open(setupFiles.consulAclBootstrap)
		assert.NoError(t, err)
		consulFileConfigStat, err := consulFileConfig.Stat()
		assert.NoError(t, err)
		consulCAKeyStat, err := consulCAKey.Stat()
		assert.NoError(t, err)
		consulCertificateStat, err := consulCertificate.Stat()
		assert.NoError(t, err)
		consulAclBootstrapStat, err := consulAclBootstrap.Stat()
		assert.NoError(t, err)

		assert.NoError(t, tarWriter.AddFile(
			consulFileConfig,
			consulFileConfigStat,
			ConsulCAFile,
			config.ConsulHome,
		))
		assert.NoError(t, tarWriter.AddFile(
			consulCAKey,
			consulCAKeyStat,
			ConsulCertificateKey,
			config.ConsulHome,
		))
		assert.NoError(t, tarWriter.AddFile(
			consulCertificate,
			consulCertificateStat,
			ConsulCertificate,
			config.ConsulHome,
		))
		assert.NoError(t, tarWriter.AddFile(
			consulAclBootstrap,
			consulAclBootstrapStat,
			ConsulAclBootstrap,
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
			"-ca",
			mock.AnythingOfType("string"),
		).Return(tlsCertCreateMock)

		ldapMockHandler := new(mocks.LdapHandler)
		ldapMockHandler.On("CheckServerAvailability", true).Return(nil)
		ldapMockHandler.
			On("AddService", "mailbox-1.example.com", zimbra.ServiceDiscoverServiceName).
			Return(nil)
		businessDep.On("LdapHandler", mock.Anything).Return(ldapMockHandler)
		systemdUnitMock := new(mocks4.UnitManager)
		systemdUnitMock.On("EnableUnitFiles", []string{"service-discover.service"}, false, false).Return(false, nil, nil)
		systemdUnitMock.On("Close").Return(nil)
		businessDep.On("SystemdUnitHandler").Return(systemdUnitMock, nil)

		_, err = s.importSetup(businessDep)
		assert.NoError(t, err)
	})
}

func TestCreateTLSCertificate(t *testing.T) {
	t.Parallel()

	type setupData struct {
		consulHome string
		caFile     string
	}
	setup := func(name string) (*setupData, func()) {
		consulFile := test.GenerateRandomFile(name)
		consulHome := test.GenerateRandomFolder(name)

		return &setupData{
				consulHome: consulHome,
				caFile:     consulFile.Name(),
			},
			func() {
				err := os.RemoveAll(consulFile.Name())
				err = os.RemoveAll(consulHome)
				if err != nil {
					panic(err)
				}
			}
	}

	t.Run("Works correctly", func(t *testing.T) {
		setupData, cleanup := setup("Works correctly")
		defer cleanup()
		mockCmd := new(mocks3.Cmd)
		mockCmd.On("Output").Return([]byte("random"), nil)
		businessDep := new(mocks2.BusinessDependencies)
		certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
		businessDep.On("CreateCommand",
			"/usr/bin/consul",
			"tls",
			"cert",
			"create",
			certificateDaysFlag,
			"-server",
			"-ca",
			setupData.caFile,
		).Return(mockCmd)
		s := Setup{
			ConsulHome: setupData.consulHome,
		}
		file, err := os.Create(setupData.caFile)
		assert.NoError(t, err)
		assert.NoError(t, s.createTLSCertificate(businessDep, file))
	})

	t.Run("Error should propagate if command fails", func(t *testing.T) {
		setupData, cleanup := setup("Works correctly")
		defer cleanup()
		mockCmd := new(mocks3.Cmd)
		expectedErrorMessage := "this is an error"
		mockCmd.On("Output").Return(nil, errors.New(expectedErrorMessage))
		businessDep := new(mocks2.BusinessDependencies)
		certificateDaysFlag := fmt.Sprintf("-days=%d", certificateExpiration)
		businessDep.On("CreateCommand",
			"/usr/bin/consul",
			"tls",
			"cert",
			"create",
			certificateDaysFlag,
			"-server",
			"-ca",
			setupData.caFile,
		).Return(mockCmd)
		s := Setup{
			ConsulHome: setupData.consulHome,
		}
		file, err := os.Create(setupData.caFile)
		assert.NoError(t, err)
		assert.EqualError(
			t,
			s.createTLSCertificate(businessDep, file),
			fmt.Sprintf("unable to generate correct CA certificate: %s", expectedErrorMessage),
		)
	})
}

func TestExecInPath(t *testing.T) {
	t.Parallel()

	t.Run("Error if chdir in a non-existing dir", func(t *testing.T) {
		businessDep := new(mocks2.BusinessDependencies)
		nonExistingFolder := test.GenerateRandomFolder("Error if chdir in a non-existing dir")
		assert.NoError(t, os.RemoveAll(nonExistingFolder))
		s := Setup{}
		assert.EqualError(
			t,
			s.execInPath(businessDep, nonExistingFolder, "it", "doesn't", "matter"),
			fmt.Sprintf("chdir %s: no such file or directory", nonExistingFolder),
		)
	})

	t.Run("Error is reported if command fails", func(t *testing.T) {
		mockCmd := new(mocks3.Cmd)
		mockCmd.On("Output").Return(nil, errors.New("this is an error message"))
		businessDep := new(mocks2.BusinessDependencies)
		existingFolder := test.GenerateRandomFolder("Works correctly in an existing dir")
		defer os.RemoveAll(existingFolder)
		businessDep.On("CreateCommand",
			"/usr/bin/consul",
			"version",
		).Return(mockCmd)
		s := Setup{}
		assert.EqualError(
			t,
			s.execInPath(businessDep, existingFolder, "/usr/bin/consul", "version"),
			fmt.Sprint("this is an error message"),
		)
	})

	t.Run("Works correctly in an existing dir", func(t *testing.T) {
		mockCmd := new(mocks3.Cmd)
		mockCmd.On("Output").Return([]byte("something"), nil)
		businessDep := new(mocks2.BusinessDependencies)
		existingFolder := test.GenerateRandomFolder("Works correctly in an existing dir")
		defer os.RemoveAll(existingFolder)
		businessDep.On("CreateCommand",
			"/usr/bin/consul",
			"version",
		).Return(mockCmd)
		s := Setup{}
		assert.NoError(t, s.execInPath(businessDep, existingFolder, "/usr/bin/consul", "version"))
	})
}

func TestSetup_openClusterCredential(t *testing.T) {
	t.Parallel()

	t.Run("File doesn't exists", func(t *testing.T) {
		nonExistingFile := test.GenerateRandomFile("File doesn't exists")
		assert.NoError(t, os.Remove(nonExistingFile.Name()))
		s := &Setup{
			ClusterCredential: nonExistingFile.Name(),
		}
		_, err := s.openClusterCredential()
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"cannot find Cluster credential in %s, please copy the file from the existing server",
				s.ClusterCredential,
			),
		)
	})

	t.Run("File exists", func(t *testing.T) {
		existingFile := test.GenerateRandomFile("File exists")
		defer os.Remove(existingFile.Name())
		s := &Setup{
			ClusterCredential: existingFile.Name(),
		}
		_, err := s.openClusterCredential()
		assert.NoError(t, err)
	})
}

func TestSetup_extractNecessaryFilesFromArchive(t *testing.T) {
	t.Parallel()

	type setupData struct {
		clusterCredential string
		caFile            string
	}
	setup := func(name string) (*setupData, func()) {
		clusterCredential := test.GenerateRandomFile(name)
		caFile := test.GenerateRandomFile(name)

		return &setupData{
				clusterCredential: clusterCredential.Name(),
				caFile:            caFile.Name(),
			}, func() {
				err := os.Remove(clusterCredential.Name())
				err = os.Remove(caFile.Name())
				if err != nil {
					panic(err)
				}
			}
	}

	t.Run("Not all files are present", func(t *testing.T) {
		// We include only CaFile, but here we need other ones too
		setupData, cleanup := setup("Not all files are present")
		defer cleanup()

		err := ioutil.WriteFile(setupData.caFile, []byte("data"), os.FileMode(0644))
		assert.NoError(t, err)
		clusterCredentialFile, err := os.Create(setupData.clusterCredential)
		assert.NoError(t, err)
		credWriter, err := credentialsEncrypter.NewWriter(clusterCredentialFile, []byte("password"))
		caFile, err := os.Open(setupData.caFile)
		assert.NoError(t, err)
		caFileStat, err := caFile.Stat()
		assert.NoError(t, err)
		assert.NoError(t, credWriter.AddFile(caFile, caFileStat, ConsulCAFile, "/"))
		assert.NoError(t, credWriter.Close())
		_, err = clusterCredentialFile.Seek(0, io.SeekStart)
		assert.NoError(t, err)

		s := &Setup{
			ClusterCredential: setupData.clusterCredential,
		}
		_, err = s.extractNecessaryFilesFromArchive(clusterCredentialFile, "password")
		assert.EqualError(t, err, fmt.Sprintf(
			"not all required files where detected in %s",
			setupData.clusterCredential,
		))
	})

	t.Run("All files are present", func(t *testing.T) {
		// We include only CaFile, but here we need other ones too
		setupData, cleanup := setup("All files are present")
		defer cleanup()

		expectedContent := []byte("you made it king")
		err := ioutil.WriteFile(setupData.caFile, expectedContent, os.FileMode(0644))
		assert.NoError(t, err)
		clusterCredentialFile, err := os.Create(setupData.clusterCredential)
		assert.NoError(t, err)
		credWriter, err := credentialsEncrypter.NewWriter(clusterCredentialFile, []byte("password"))
		caFile, err := os.Open(setupData.caFile)
		assert.NoError(t, err)
		caFileStat, err := caFile.Stat()
		assert.NoError(t, err)
		assert.NoError(t, credWriter.AddFile(caFile, caFileStat, ConsulCAFile, "/"))
		_, err = caFile.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		assert.NoError(t, credWriter.AddFile(caFile, caFileStat, ConsulAclBootstrap, "/"))
		_, err = caFile.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		assert.NoError(t, credWriter.AddFile(caFile, caFileStat, ConsulCertificateKey, "/"))
		assert.NoError(t, credWriter.Close())
		_, err = clusterCredentialFile.Seek(0, io.SeekStart)
		assert.NoError(t, err)

		s := &Setup{
			ClusterCredential: setupData.clusterCredential,
		}
		outputBs, err := s.extractNecessaryFilesFromArchive(clusterCredentialFile, "password")
		assert.NoError(t, err)
		assert.Equal(t, string(expectedContent), string(outputBs))
	})
}
