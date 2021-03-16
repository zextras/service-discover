package setup

import (
	//"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	mocks2 "bitbucket.org/zextras/service-discover/cli/server/command/setup/mocks"
	"bitbucket.org/zextras/service-discover/cli/server/exec"
	"bitbucket.org/zextras/service-discover/cli/server/mocks"
	mocks3 "bitbucket.org/zextras/service-discover/cli/server/systemd/mocks"
	"syscall"

	//"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/test"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	//"bitbucket.org/zextras/service-discover/cli/server/exec"
	//"bitbucket.org/zextras/service-discover/cli/server/systemd"
	"bytes"
	"crypto/rand"

	//"github.com/sethvargo/go-password/password"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"io"
	"io/ioutil"
	"net"
	"os"
	native_exec "os/exec"
	"path/filepath"
	"testing"
	"time"
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

func Test_addrsToSingleString(t *testing.T) {
	t.Parallel()
	separator := ", "

	type args struct {
		addrs *[]net.Addr
		sep   string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Single interface",
			args: args{
				addrs: &[]net.Addr{
					&addrStub{ip: "127.0.0.1"},
				},
				sep: separator,
			},
			want: "127.0.0.1",
		},
		{
			name: "Multiple interfaces",
			args: args{
				addrs: &[]net.Addr{
					&addrStub{ip: "127.0.0.1"},
					&addrStub{ip: "10.0.0.1"},
				},
				sep: separator,
			},
			want: "127.0.0.1, 10.0.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, addrsToSingleString(tt.args.addrs, tt.args.sep))
		})
	}
}

type mocked interface {
	On(methodName string, arguments ...interface{}) *mock.Call
}

//	TODO		name: "First setup without rootUid",
func TestFirstSetup_business(t *testing.T) {
	t.Run("Complete all setup tasks", func(t *testing.T) {
		var cleanups = make([]func(), 0)
		defer func() {
			for _, f := range cleanups {
				f()
			}
		}()

		setup, setupCleanup := createSetup(t)
		cleanups = append(cleanups, setupCleanup)

		mockLocalConfig := new(mocks.LocalConfig)
		mockLdapHandler := new(mocks.LdapHandler)
		mockSystemdUnit := new(mocks3.UnitManager)
		mockDependencies := new(mocks2.BusinessDependencies)
		mockDependencies.On("Writer").Return(ioutil.Discard)
		mockNetwork(mockDependencies, false, false)
		cleanup := mockBusinessDependencies(&setup, mockDependencies, mockLocalConfig, mockLdapHandler, mockSystemdUnit)
		cleanups = append(cleanups, cleanup)

		out, err := setup.firstSetup(mockDependencies)

		assert.NoError(t, err)
		assert.NotNil(t, out)
		text, err := out.PlainRender()
		assert.NoError(t, err)
		assert.Equal(t, "", text)
		text, err = out.JsonRender()
		assert.NoError(t, err)
		assert.Equal(t, "{\"cluster_credentials\":\""+setup.ClusterCredential+"\"}", text)

		mockLdapHandler.AssertNumberOfCalls(t, "AddService", 1)
		mockDependencies.AssertNumberOfCalls(t, "CreateCommand", 3)

		clusterCredentialFile, err := os.Open(setup.ClusterCredential)
		assert.NoError(t, err, "File should exist")
		defer clusterCredentialFile.Close() // It will be removed in the cleanup() function deferred before
		encReader, err := credentialsEncrypter.NewReader(clusterCredentialFile, []byte("password"))
		assert.NoError(t, err, "Error while opening tar reader")
		listOfCompressedFiles := make([]string, 0)
		for {
			header, err := encReader.Next()
			if err == io.EOF {
				t.Log("Reached EOF")
				break
			}
			assert.NoError(t, err, "Error while reading tar file")
			t.Logf("Header name file: %s\n", header.Name)
			listOfCompressedFiles = append(listOfCompressedFiles, header.Name)
		}

		// Note: we use relative path since the tarball will not have absolute paths.
		expectedFileList := make([]string, 0)
		caFileNameRel, _ := filepath.Rel("/", setup.ConsulHome+"/"+ConsulCAFile)
		cCertificateRel, _ := filepath.Rel("/", setup.ConsulHome+"/"+ConsulCertificate)
		cCAKeyFileNameRel, _ := filepath.Rel("/", setup.ConsulHome+"/"+ConsulCertificateKey)
		expectedFileList = append(expectedFileList, ConsulAclBootstrap)
		expectedFileList = append(expectedFileList, caFileNameRel)
		expectedFileList = append(expectedFileList, cCertificateRel)
		expectedFileList = append(expectedFileList, cCAKeyFileNameRel)
		assert.Equal(
			t,
			len(expectedFileList),
			len(listOfCompressedFiles),
			"The number of elements in the array is not the wanted one",
		)
		assert.ElementsMatch(
			t,
			expectedFileList,
			listOfCompressedFiles,
			"The element in the arrays are not equal",
		)
	})

	t.Run("First non interactive setup without lo interface", func(t *testing.T) {
		var cleanups = make([]func(), 0)
		defer func() {
			for _, f := range cleanups {
				f()
			}
		}()

		setup, setupCleanup := createSetup(t)
		cleanups = append(cleanups, setupCleanup)

		mockLocalConfig := new(mocks.LocalConfig)
		mockLdapHandler := new(mocks.LdapHandler)
		mockSystemdUnit := new(mocks3.UnitManager)
		mockDependencies := new(mocks2.BusinessDependencies)
		mockDependencies.On("Writer").Return(ioutil.Discard)
		mockNetwork(mockDependencies, true, false)
		cleanup := mockBusinessDependencies(&setup, mockDependencies, mockLocalConfig, mockLdapHandler, mockSystemdUnit)
		cleanups = append(cleanups, cleanup)

		out, err := setup.firstSetup(mockDependencies)

		assert.NoError(t, err)
		assert.NotNil(t, out)
	})
}

func mockBusinessDependencies(
	setup *Setup,
	mockDependencies *mocks2.BusinessDependencies,
	mockLocalConfig *mocks.LocalConfig,
	mockLdapHandler *mocks.LdapHandler,
	mockSystemdUnit *mocks3.UnitManager,
) func() {
	mockDependencies.On("GetuidSyscall").Return(0)
	mockDependencies.On(
		"CreateCommand",
		"/usr/bin/consul",
		"tls",
		"ca",
		"create",
		"-days=10950",
		"-name-constraint",
	).Return(
		exec.Command(
			"/usr/bin/consul",
			"tls",
			"ca",
			"create",
			"-days=10950",
			"-name-constraint",
		),
	)

	mockDependencies.On(
		"CreateCommand",
		"/usr/bin/consul",
		"tls",
		"cert",
		"create",
		"-days=10950",
		"-server",
	).Return(
		exec.Command(
			"/usr/bin/consul",
			"tls",
			"cert",
			"create",
			"-days=10950",
			"-server",
		),
	)

	mockDependencies.On(
		"CreateCommand",
		"/usr/bin/consul",
		"acl",
		"bootstrap",
		"-format",
		"json",
	).Return(
		exec.Command(
			"/usr/bin/consul",
			"acl",
			"bootstrap",
			"-format",
			"json",
		),
	)

	mockDependencies.On("SystemdUnitHandler").Return(mockSystemdUnit, nil)
	mockSystemdUnit.On("EnableUnitFiles", []string{"service-discover.service"}, false, false).Return(
		false, nil, nil,
	)

	var cleanups = make([]func(), 0)
	mockSystemdUnit.On("StartUnit", "service-discover.service", "replace", mock.Anything).Return(
		0, nil,
	).Run(func(args mock.Arguments) {
		ch := args.Get(2).(chan<- string)

		cmd := native_exec.Command(
			"/usr/bin/consul",
			"agent",
			"-dev", //otherwise it takes up to 10 seconds to boostrap
			"-config-dir",
			setup.ConsulConfigDir,
			"-server",
			"-bind",
			"127.0.0.1", //otherwise test address will be used
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
	mockSystemdUnit.On("Close").Return(nil)
	mockDependencies.On("LocalConfigLoader", setup.LocalConfigPath).Return(mockLocalConfig)
	mockDependencies.On("LdapHandler", mock.Anything).Return(mockLdapHandler)
	mockLdapHandler.On("CheckServerAvailability", true).Return(nil)
	mockLdapHandler.On("AddService", "mailbox-1.example.com", "service-discover").Return(nil)
	return func() {
		for _, f := range cleanups {
			f()
		}
	}
}

func createSetup(t *testing.T) (Setup, func()) {
	tmpDir := t.TempDir()
	setup := Setup{
		ConsulConfigDir:   tmpDir + "/config",
		ConsulHome:        tmpDir + "/home",
		LocalConfigPath:   tmpDir + "/localconfig.xml",
		ConsulData:        tmpDir + "/home/data",
		ConsulFileConfig:  tmpDir + "/config/main.json",
		ClusterCredential: tmpDir + "/config/credentials.tar.pgp",
		MutableConfigFile: tmpDir + "/config/mutable.json",

		Wizard:        true,
		FirstInstance: true,
		Password:      "password",
		BindAddress:   "10.0.0.1",
	}

	var err error
	err = os.MkdirAll(setup.ConsulConfigDir, 0700)
	if err != nil {
		panic(err)
	}
	err = os.MkdirAll(setup.ConsulHome, 0700)
	if err != nil {
		panic(err)
	}
	err = os.MkdirAll(setup.ConsulData, 0700)
	if err != nil {
		panic(err)
	}

	localConfig := `<?xml version="1.0" encoding="UTF-8"?>
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

	err = ioutil.WriteFile(setup.LocalConfigPath, []byte(localConfig), 0644)
	if err != nil {
		panic(err)
	}

	return setup, func() {
		err := os.RemoveAll(tmpDir)
		if err != nil {
			panic("cannot remove " + tmpDir + ": " + err.Error())
		}
	}
}

func TestFirstSetup_inputs(t *testing.T) {
	t.Parallel()

	t.Run("Gather all inputs in interactive mode", func(t *testing.T) {
		out := new(bytes.Buffer)
		mockDependencies := new(mocks2.InteractiveDependencies)
		mockDependencies.On("Reader").Return(populateWithFirstClusterInput("10.0.0.1"))
		mockDependencies.On("Writer").Return(out)
		mockNetwork(mockDependencies, false, false)

		configurations, err := gatherInputs(mockDependencies)
		assert.NoError(t, err)
		assert.NotNil(t, configurations)

		assert.Equal(t, true, configurations.firstInstance)
		assert.Equal(t, "10.0.0.1", configurations.BindAddress)
		assert.Equal(t, "password", configurations.Password)
	})

	t.Run("Gather all inputs in interactive mode without lo interface", func(t *testing.T) {
		out := new(bytes.Buffer)
		mockDependencies := new(mocks2.InteractiveDependencies)
		mockDependencies.On("Reader").Return(populateWithFirstClusterInput("10.0.0.1"))
		mockDependencies.On("Writer").Return(out)
		mockNetwork(mockDependencies, true, false)

		configurations, err := gatherInputs(mockDependencies)
		assert.NoError(t, err)
		assert.NotNil(t, configurations)

		assert.Equal(t, true, configurations.firstInstance)
		assert.Equal(t, "10.0.0.1", configurations.BindAddress)
		assert.Equal(t, "password", configurations.Password)
	})

	t.Run("Gather all inputs in interactive mode invalid binding address", func(t *testing.T) {
		out := new(bytes.Buffer)
		mockDependencies := new(mocks2.InteractiveDependencies)
		mockDependencies.On("Reader").Return(populateWithFirstClusterInput("10.0.0.200"))
		mockDependencies.On("Writer").Return(out)
		mockNetwork(mockDependencies, true, false)

		configurations, err := gatherInputs(mockDependencies)
		assert.EqualError(t, err, "invalid binding address selected")
		assert.Nil(t, configurations)
	})

	t.Run("Gather all inputs in interactive mode subnet binding", func(t *testing.T) {
		out := new(bytes.Buffer)
		mockDependencies := new(mocks2.InteractiveDependencies)
		mockDependencies.On("Reader").Return(populateWithFirstClusterInput("10.0.0.1"))
		mockDependencies.On("Writer").Return(out)
		mockNetwork(mockDependencies, true, true)

		configurations, err := gatherInputs(mockDependencies)
		assert.NoError(t, err)
		assert.NotNil(t, configurations)
		assert.Equal(t, true, configurations.firstInstance)
		assert.Equal(t, "10.0.0.1", configurations.BindAddress)
		assert.Equal(t, "password", configurations.Password)
	})
}

func populateWithFirstClusterInput(bindingAddress string) *bytes.Buffer {
	buf := bytes.NewBuffer([]byte(`Y
` + bindingAddress + `
password
`))
	return buf
}

func mockLdap(ldap mocked) {
	ldap.On(
		"CheckServerAvailability",
		true,
	).Return(nil)

	ldap.On(
		"AddService",
		"mailbox-1.example.com",
		zimbra.ServiceDiscoverServiceName,
	).Return(nil)
}

func mockNetwork(network mocked, withoutLocalHost bool, includeSubnet bool) {
	localhost := net.Interface{
		Index:        1, // Read GoDoc about net.Interface if you're puzzled by this
		MTU:          42,
		Name:         "lo",
		HardwareAddr: []byte("00:00:00:00:00:00"),
		Flags:        0,
	}

	card0 := net.Interface{
		Index:        1, // Read GoDoc about net.Interface if you're puzzled by this
		MTU:          42,
		Name:         "eno0",
		HardwareAddr: []byte("78:bc:e6:2f:8a:d7"),
		Flags:        0,
	}

	card1 := net.Interface{
		Index:        1, // Read GoDoc about net.Interface if you're puzzled by this
		MTU:          42,
		Name:         "eno1",
		HardwareAddr: []byte("78:bc:e6:2f:8a:d9"),
		Flags:        0,
	}

	network.On("AddrResolver", localhost).Return(
		[]net.Addr{
			&addrStub{ip: "127.0.0.1"},
		},
		nil,
	)

	if includeSubnet {
		network.On("AddrResolver", card0).Return(
			[]net.Addr{
				&addrStub{ip: "10.0.0.1/8"},
			},
			nil,
		)
	} else {
		network.On("AddrResolver", card0).Return(
			[]net.Addr{
				&addrStub{ip: "10.0.0.1"},
			},
			nil,
		)
	}

	network.On("AddrResolver", card1).Return(
		[]net.Addr{
			&addrStub{ip: "10.0.0.2"},
		},
		nil,
	)

	if withoutLocalHost {
		network.On("NetInterfaces").Return(
			[]net.Interface{
				card0,
				card1,
			},
			nil,
		)
	} else {
		network.On("NetInterfaces").Return(
			[]net.Interface{
				localhost,
				card0,
				card1,
			},
			nil,
		)
	}
}

type mockedNetworkInterfaces struct {
	mock.Mock
}

func (m *mockedNetworkInterfaces) AddrResolver(n net.Interface) ([]net.Addr, error) {
	args := m.Called(n)
	return args.Get(0).([]net.Addr), args.Error(1)
}

func (m *mockedNetworkInterfaces) NetInterfaces() ([]net.Interface, error) {
	args := m.Called()
	return args.Get(0).([]net.Interface), args.Error(1)
}

func TestSetup_checkValidBindingAddress(t *testing.T) {
	t.Parallel()

	t.Run("Valid interface selected", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1"},
				&addrStub{ip: "10.0.0.1"},
			},
			nil,
		)
		err := checkValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"127.0.0.1",
		)
		assert.NoError(t, err)
	})

	t.Run("Invalid interface selected", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1"},
			},
			nil,
		)
		err := checkValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"192.168.1.2", //random one, it doesn't really matter
		)
		assert.EqualError(t, err, "invalid binding address selected")
	})

	t.Run("Valid address selected with subnet", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1/24"},
			}, nil,
			nil,
		)
		err := checkValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"127.0.0.1",
		)
		assert.NoError(t, err)
	})

	t.Run("Valid subnet selected with subnet", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1/24"},
			},
			nil,
		)
		err := checkValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"127.0.0.1/24",
		)
		assert.NoError(t, err)
	})

	t.Run("Invalid subnet selected with subnet", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1/24"},
			}, nil,
		)
		err := checkValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"10.0.0.1/8",
		)
		assert.EqualError(t, err, "invalid binding address selected")
	})
}

func TestSetup_createEncryptedSecret(t *testing.T) {
	type fields struct {
		ClusterCredential string
		NumberOfFiles     int
	}
	type args struct {
		filesToCompress map[string]string
		password        string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "One file archive creation",
			fields: fields{
				ClusterCredential: test.GenerateRandomFile("One file archive creation").Name(),
				NumberOfFiles:     1,
			},
			args: args{
				password: "password",
			},
			wantErr: false,
		},
		{
			name: "Multiple file archive creation",
			fields: fields{
				ClusterCredential: test.GenerateRandomFile("One file archive creation").Name(),
				NumberOfFiles:     3,
			},
			args: args{
				password: "password",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer os.Remove(tt.fields.ClusterCredential)
			s := &Setup{
				ClusterCredential: tt.fields.ClusterCredential,
			}
			filesToInclude := make(map[string]string, 0)
			defer func() {
				for _, path := range filesToInclude {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
			}()
			for i := 0; i < tt.fields.NumberOfFiles; i++ {
				file := test.GenerateRandomFile(tt.name)
				defer file.Close()
				content := make([]byte, (i+1)*4096)
				_, err := rand.Read(content)
				if err != nil {
					panic(err)
				}
				err = ioutil.WriteFile(file.Name(), content, 0644)
				if err != nil {
					panic(err)
				}
				filesToInclude[filepath.Base(file.Name())] = file.Name()
			}
			if tt.wantErr {
				assert.Error(t, s.createEncryptedSecret(filesToInclude, tt.args.password))
			} else {
				assert.NoError(t, s.createEncryptedSecret(filesToInclude, tt.args.password))
				// We need to assert the content of the files are correct too
				cc, err := os.Open(tt.fields.ClusterCredential)
				assert.NoError(t, err)
				reader, err := credentialsEncrypter.NewReader(cc, []byte(tt.args.password))
				assert.NoError(t, err)
				actualNumberOfFiles := 0
				for {
					header, err := reader.Next()
					if err == io.EOF {
						t.Log("Reached EOF")
						break
					}
					assert.NoError(t, err, "Error while reading tar file")
					t.Logf("Header name file: %s\n", header.Name)
					actualBytesBuf := &bytes.Buffer{}
					_, err = io.Copy(actualBytesBuf, reader)
					assert.NoError(t, err)
					expectedBytes, err := ioutil.ReadFile(filesToInclude[header.Name])
					assert.NoError(t, err)
					actualBytes, err := ioutil.ReadAll(actualBytesBuf)
					assert.NoError(t, err)
					assert.Equal(
						t,
						expectedBytes,
						actualBytes,
						"The content of the two files are different",
					)
					actualNumberOfFiles++
				}
				assert.Equal(
					t,
					tt.fields.NumberOfFiles,
					actualNumberOfFiles,
					"The number of elements are different",
				)
			}
		})
	}
}
