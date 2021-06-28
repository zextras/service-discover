package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/credentialsEncrypter"
	"bitbucket.org/zextras/service-discover/cli/lib/exec"
	mocks4 "bitbucket.org/zextras/service-discover/cli/lib/exec/mocks"
	mocks3 "bitbucket.org/zextras/service-discover/cli/lib/systemd/mocks"
	"bitbucket.org/zextras/service-discover/cli/lib/term/mocks"
	mocks5 "bitbucket.org/zextras/service-discover/cli/lib/zimbra/mocks"
	mocks2 "bitbucket.org/zextras/service-discover/cli/server/command/setup/mocks"
	"syscall"

	"bitbucket.org/zextras/service-discover/cli/lib/test"
	"bitbucket.org/zextras/service-discover/cli/lib/zimbra"
	"bytes"
	"crypto/rand"

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

		mockLocalConfig, err := zimbra.LoadLocalConfig(setup.LocalConfigPath)
		assert.NoError(t, err)
		mockLdapHandler := new(mocks5.LdapHandler)
		mockSystemdUnit := new(mocks3.UnitManager)
		mockDependencies := new(mocks2.BusinessDependencies)
		mockDependencies.On("writer").Return(ioutil.Discard)
		mockNetwork(mockDependencies, false, false)
		cleanup := mockBusinessDependencies(&setup, mockDependencies, &mockLocalConfig, mockLdapHandler, mockSystemdUnit)
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
		mockDependencies.AssertNumberOfCalls(t, "CreateCommand", 6)

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
		caFileNameRel, _ := filepath.Rel("/", setup.ConsulHome+"/"+command.ConsulCA)
		caKeyFileNameRel, _ := filepath.Rel("/", setup.ConsulHome+"/"+command.ConsulCAKey)

		expectedFileList = append(expectedFileList, command.GossipKey)
		expectedFileList = append(expectedFileList, command.ConsulAclBootstrap)
		expectedFileList = append(expectedFileList, caKeyFileNameRel)
		expectedFileList = append(expectedFileList, caFileNameRel)
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

		mockLocalConfig, err := zimbra.LoadLocalConfig(setup.LocalConfigPath)
		assert.NoError(t, err)
		mockLdapHandler := new(mocks5.LdapHandler)
		mockSystemdUnit := new(mocks3.UnitManager)
		mockDependencies := new(mocks2.BusinessDependencies)
		mockDependencies.On("writer").Return(ioutil.Discard)
		mockNetwork(mockDependencies, true, false)
		cleanup := mockBusinessDependencies(&setup, mockDependencies, &mockLocalConfig, mockLdapHandler, mockSystemdUnit)
		cleanups = append(cleanups, cleanup)

		out, err := setup.firstSetup(mockDependencies)

		assert.NoError(t, err)
		assert.NotNil(t, out)
	})
}

func mockBusinessDependencies(
	setup *Setup,
	mockDependencies *mocks2.BusinessDependencies,
	mockLocalConfig *zimbra.LocalConfig,
	mockLdapHandler *mocks5.LdapHandler,
	mockSystemdUnit *mocks3.UnitManager,
) func() {
	var cleanups = make([]func(), 0)
	aclPolicyCreateMock := new(mocks4.Cmd)
	aclPolicyCreateMock.On("Output").Return([]byte("something"), nil)
	tokenCreateMock := new(mocks4.Cmd)
	tokenCreateMock.On("Output").Return([]byte(`{
		  "SecretID": "secret-token-2"
		}`), nil)
	setTokenCmd := new(mocks4.Cmd)
	setTokenCmd.On("Output").Return(make([]byte, 0), nil)
	mockDependencies.On("GetuidSyscall").Return(0).
		On("CreateCommand",
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
}`).
		Return(aclPolicyCreateMock, nil).
		On("CreateCommand",
			"/usr/bin/consul",
			"acl",
			"token",
			"create",
			"-policy-name",
			"server-mailbox-1-example-com",
			"-format",
			"json").
		Return(tokenCreateMock).
		On("CreateCommand",
			"/usr/bin/consul",
			"acl",
			"set-agent-token",
			"default",
			"secret-token-2",
		).Return(setTokenCmd).
		On(
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
	).On(
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
	).On(
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
	).On("SystemdUnitHandler").Return(mockSystemdUnit, nil).
		On("LocalConfigLoader", setup.LocalConfigPath).Return(mockLocalConfig).
		On("LdapHandler", mock.Anything).Return(mockLdapHandler)
	mockSystemdUnit.On("EnableUnitFiles", []string{"service-discover.service"}, false, false).Return(
		false, nil, nil,
	).On("StartUnit", "service-discover.service", "replace", mock.Anything).Return(
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
	}).On("Close").Return(nil)
	mockLdapHandler.On("CheckServerAvailability", true).Return(nil).
		On("AddService", "mailbox-1.example.com", "service-discover").Return(nil)
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

	err = ioutil.WriteFile(setup.LocalConfigPath, test.GenerateLocalConfig(
		t,
		"mailbox-1.example.com",
		"ldap://mailbox-1.example.com:389",
		"ldap://mailbox-1.example.com:389",
		test.DefaultLdapUserDN,
		"pa$$word",
	), 0644)
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
		mockTerm := new(mocks.Terminal)
		mockTerm.On("Write", mock.AnythingOfType("[]uint8")).Run(func(args mock.Arguments) {
			out.Write(args.Get(0).([]byte))
		}).Return(0, nil).
			On("ReadLine").
			Return("", nil).
			Return("10.0.0.1", nil).
			On("ReadPassword", mock.AnythingOfType("string")).
			Return("password", nil)
		mockDependencies := new(mocks2.InteractiveDependencies)
		mockDependencies.On("Term").Return(mockTerm)
		mockNetwork(mockDependencies, false, false)

		configurations, err := gatherInputs(mockDependencies)
		assert.NoError(t, err)
		assert.NotNil(t, configurations)

		// This will always be false since gatherInputs doesn't take provide FirstInstance anymore
		assert.Equal(t, false, configurations.FirstInstance)
		assert.Equal(t, "10.0.0.1", configurations.BindAddress)
		assert.Equal(t, "password", configurations.Password)
		allOut, _ := io.ReadAll(out)
		assert.Equal(t, `Multiple network cards detected:
eno1 10.0.0.2
eno0 10.0.0.1
Specify the binding address for service discovery: `, string(allOut))
	})

	t.Run("Gather all inputs in interactive mode without lo interface", func(t *testing.T) {
		out := new(bytes.Buffer)
		mockTerm := new(mocks.Terminal)
		mockTerm.On("Write", mock.AnythingOfType("[]uint8")).Run(func(args mock.Arguments) {
			out.Write(args.Get(0).([]byte))
		}).Return(0, nil).
			On("ReadLine").
			Return("", nil).
			Return("10.0.0.1", nil).
			On("ReadPassword", mock.AnythingOfType("string")).
			Return("password", nil)
		mockDependencies := new(mocks2.InteractiveDependencies)
		mockDependencies.On("Term").Return(mockTerm)
		mockNetwork(mockDependencies, true, false)

		configurations, err := gatherInputs(mockDependencies)
		assert.NoError(t, err)
		assert.NotNil(t, configurations)

		// This will always be false since gatherInputs doesn't take provide FirstInstance anymore
		assert.Equal(t, false, configurations.FirstInstance)
		assert.Equal(t, "10.0.0.1", configurations.BindAddress)
		assert.Equal(t, "password", configurations.Password)
		allOut, _ := io.ReadAll(out)
		assert.Equal(t, `Multiple network cards detected:
eno0 10.0.0.1
eno1 10.0.0.2
Specify the binding address for service discovery: `, string(allOut))
	})

	t.Run("Gather all inputs in interactive mode invalid binding address", func(t *testing.T) {
		out := new(bytes.Buffer)
		mockTerm := new(mocks.Terminal)
		mockTerm.On("Write", mock.AnythingOfType("[]uint8")).Run(func(args mock.Arguments) {
			out.Write(args.Get(0).([]byte))
		}).Return(0, nil).
			On("ReadLine").
			Return("", nil).
			Return("10.0.0.200", nil).
			On("ReadPassword", mock.AnythingOfType("string")).
			Return("password", nil)
		mockDependencies := new(mocks2.InteractiveDependencies)
		mockDependencies.On("Term").Return(mockTerm)
		mockNetwork(mockDependencies, true, false)

		configurations, err := gatherInputs(mockDependencies)
		assert.EqualError(t, err, "invalid binding address selected")
		assert.Nil(t, configurations)
		allOut, _ := io.ReadAll(out)
		assert.Equal(t, `Multiple network cards detected:
eno0 10.0.0.1
eno1 10.0.0.2
Specify the binding address for service discovery: `, string(allOut))
	})

	t.Run("Gather all inputs in interactive mode subnet binding", func(t *testing.T) {
		out := new(bytes.Buffer)
		mockTerm := new(mocks.Terminal)
		mockTerm.On("Write", mock.AnythingOfType("[]uint8")).Run(func(args mock.Arguments) {
			out.Write(args.Get(0).([]byte))
		}).Return(0, nil).
			On("ReadLine").
			Return("", nil).
			Return("10.0.0.1", nil).
			On("ReadPassword", mock.AnythingOfType("string")).
			Return("password", nil)
		mockDependencies := new(mocks2.InteractiveDependencies)
		mockDependencies.On("Term").Return(mockTerm)
		mockNetwork(mockDependencies, true, true)

		configurations, err := gatherInputs(mockDependencies)
		assert.NoError(t, err)
		assert.NotNil(t, configurations)

		// This will always be false since gatherInputs doesn't take provide FirstInstance anymore
		assert.Equal(t, false, configurations.FirstInstance)
		assert.Equal(t, "10.0.0.1", configurations.BindAddress)
		assert.Equal(t, "password", configurations.Password)
		allOut, _ := io.ReadAll(out)
		assert.Equal(t, `Multiple network cards detected:
eno0 10.0.0.1/8
eno1 10.0.0.2
Specify the binding address for service discovery: `, string(allOut))
	})
}

func populateWithFirstClusterInput(bindingAddress string) *bytes.Buffer {
	buf := bytes.NewBuffer([]byte(bindingAddress + `
password
`))
	return buf
}

func mockLdap(ldap mocked) {
	ldap.On(
		"CheckServerAvailability",
		true,
	).Return(nil).
		On(
			"AddService",
			"mailbox-1.example.com",
			zimbra.ServiceDiscoverServiceName,
		).Return(nil).
		On(
			"QueryAllServersWithService",
			zimbra.ServiceDiscoverServiceName,
		).Return(0, nil)
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

	network.On("LookupIP", "mailbox-1.example.com").Return([]net.IP{net.IPv4(1, 1, 1, 1)}, nil)
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
