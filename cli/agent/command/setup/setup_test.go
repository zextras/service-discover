/*
 * Copyright (C) 2023 Zextras srl
 *
 *     This program is free software: you can redistribute it and/or modify
 *     it under the terms of the GNU Affero General Public License as published by
 *     the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU Affero General Public License for more details.
 *
 *     You should have received a copy of the GNU Affero General Public License
 *     along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 */

package setup

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zextras/service-discover/cli/agent/command/setup/mocks"
	"github.com/Zextras/service-discover/cli/lib/carbonio"
	mocks5 "github.com/Zextras/service-discover/cli/lib/carbonio/mocks"
	"github.com/Zextras/service-discover/cli/lib/command"
	"github.com/Zextras/service-discover/cli/lib/credentialsEncrypter"
	mocks2 "github.com/Zextras/service-discover/cli/lib/exec/mocks"
	mocks4 "github.com/Zextras/service-discover/cli/lib/systemd/mocks"
	"github.com/Zextras/service-discover/cli/lib/test"
	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/go-ldap/ldap/v3"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/testcontainers/testcontainers-go"
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

	t.Run("Should not fail if cluster credential file is missing", func(t *testing.T) {
		clusterCredentialPath := "bogus-value"
		missingConsul := new(mocks2.Cmd)
		missingConsul.On("Run").Return(nil)
		mockedDep := new(mocks.BusinessDependencies)
		mockedDep.
			On("CreateCommand", command.ConsulBin, "version").
			Return(missingConsul).
			On("GetuidSyscall").
			Return(0)
		assert.NoError(t,
			preRun(clusterCredentialPath, mockedDep),
			"Pre run should not give Cluster-credential missing error",
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
	testingMode = true

	type testDependencies struct {
		FakeLocalConfig           *os.File
		ClusterCredentialDownload *os.File
		Container                 testcontainers.Container
		CtxContainer              context.Context
	}
	setup := func(t *testing.T, testName string, credentialsContent []byte) (*testDependencies, func()) {
		t.Log("Starting LDAP container")
		container, ctxContainer := test.SpinUpCarbonioLdap(t, test.PUBLIC_IMAGE_ADDRESS, test.LATEST_RELEASE)
		containerIP, err := container.ContainerIP(ctxContainer)
		t.Logf("LDAP container started at %s", containerIP)
		if err != nil {
			t.Error(err)
		}
		localConfigByte := test.GenerateLocalConfig(
			t,
			containerIP,
			"ldap://"+containerIP+":389",
			"ldap://"+containerIP+":389",
			test.DefaultLdapUserDN,
			"password",
		)
		file, err := os.CreateTemp("", testName+"*")
		if err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(file.Name(), localConfigByte, 0744); err != nil {
			t.Error(err)
		}
		connection, err := ldap.DialURL("ldap://"+containerIP+":389", ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Minute}))
		if err != nil {
			t.Error(err)
		}
		if err := connection.Bind(test.DefaultLdapUserDN, "password"); err != nil {
			t.Error(err)
		}

		encodedContent := base64.StdEncoding.EncodeToString(credentialsContent)
		modRequest := ldap.NewModifyRequest("cn=config,cn=zimbra", []ldap.Control{})
		modRequest.Replace("carbonioMeshCredentials", []string{encodedContent})
		err = connection.Modify(modRequest)
		assert.NoError(t, err)

		clusterCredentialDownloadFile := test.GenerateRandomFile(testName)

		return &testDependencies{
				file,
				clusterCredentialDownloadFile,
				container,
				ctxContainer,
			}, func() {
				defer func(container testcontainers.Container, ctx context.Context) {
					err := container.Terminate(ctx)
					if err != nil {
						t.Error(err)
					}
				}(container, ctxContainer)

				defer func(name string) {
					err := os.Remove(name)
					if err != nil {
						t.Error(err)
					}
				}(file.Name())

				defer func(name string) {
					err := os.Remove(name)
					if err != nil {
						t.Error(err)
					}
				}(clusterCredentialDownloadFile.Name())
			}
	}

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

	t.Run("Should fail when cluster credentials are corrupted", func(t *testing.T) {
		testStruct, cleanup := setup(t, "shouldFailWhenClusterCredentialsAreMissing", []byte("this is a test"))
		defer cleanup()
		selectedInterface := net.Interface{
			Name: "en1",
		}
		localConfig, err := carbonio.LoadLocalConfig(testStruct.FakeLocalConfig.Name())
		if err != nil {
			t.Error(err)
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
		}, nil).On("LdapHandler", mock.Anything).Return(carbonio.CreateNewHandler(localConfig))
		s := &Setup{
			ClusterCredential: testStruct.ClusterCredentialDownload.Name(),
			BindAddress:       "192.168.1.1",
			LocalConfigPath:   testStruct.FakeLocalConfig.Name(),
		}
		// We have to simulate the file doesn't exist anymore
		assert.NoError(t, os.Remove(testStruct.ClusterCredentialDownload.Name()))
		_, err = s.setup(mockDep)
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"unable to read %s: EOF",
				testStruct.ClusterCredentialDownload.Name(),
			),
		)
	})

	t.Run("Should fail when a wrong password is set", func(t *testing.T) {
		contentToUpload := bytes.Buffer{}
		writer, err := credentialsEncrypter.NewWriter(&contentToUpload, []byte("password"))
		assert.NoError(t, err)
		assert.NoError(t, writer.Close())
		readContentToUpload, err := io.ReadAll(&contentToUpload)
		assert.NoError(t, err)
		testStruct, cleanup := setup(t, "Should fail when a wrong password is set", readContentToUpload)
		defer cleanup()
		selectedInterface := net.Interface{
			Name: "en1",
		}
		localConfig, err := carbonio.LoadLocalConfig(testStruct.FakeLocalConfig.Name())
		if err != nil {
			t.Error(err)
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
		}, nil).On("LdapHandler", mock.Anything).Return(carbonio.CreateNewHandler(localConfig))
		s := &Setup{
			ClusterCredential: testStruct.ClusterCredentialDownload.Name(),
			BindAddress:       "192.168.1.1",
			Password:          "wrong-password",
			LocalConfigPath:   testStruct.FakeLocalConfig.Name(),
		}
		_, err = s.setup(mockDep)
		assert.EqualError(
			t,
			err,
			fmt.Sprintf("unable to read %s: openpgp: incorrect key",
				testStruct.ClusterCredentialDownload.Name()),
		)
	})

	t.Run("Should fail to create TLS certificate if CA is not present", func(t *testing.T) {
		contentToUpload := bytes.Buffer{}
		writer, err := credentialsEncrypter.NewWriter(&contentToUpload, []byte("password"))
		assert.NoError(t, err)
		assert.NoError(t, writer.Close())
		readContentToUpload, err := io.ReadAll(&contentToUpload)
		assert.NoError(t, err)
		testStruct, cleanup := setup(t, "Should fail to create TLS certificate if CA is not present", readContentToUpload)
		defer cleanup()

		consulHome := test.GenerateRandomFolder("Should fail to create TLS certificate if CA is not present")
		defer os.RemoveAll(consulHome)
		selectedInterface := net.Interface{
			Name: "en1",
		}
		localConfig, err := carbonio.LoadLocalConfig(testStruct.FakeLocalConfig.Name())
		if err != nil {
			t.Error(err)
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
		}, nil).On("LdapHandler", mock.Anything).Return(carbonio.CreateNewHandler(localConfig))
		s := &Setup{
			ClusterCredential: testStruct.ClusterCredentialDownload.Name(),
			BindAddress:       "192.168.1.1",
			Password:          "password",
			ConsulHome:        consulHome,
			LocalConfigPath:   testStruct.FakeLocalConfig.Name(),
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
		contentToUpload := bytes.Buffer{}
		writer, err := credentialsEncrypter.NewWriter(&contentToUpload, []byte("password"))
		assert.NoError(t, err)
		errorCause := "this is the error"
		unitName := "service-discover.service"
		mutableConfiguration := test.GenerateRandomFile(testName)
		defer os.Remove(mutableConfiguration.Name())
		mutableConfig := test.GenerateRandomFile(testName)
		defer os.Remove(mutableConfig.Name())
		consulHome := test.GenerateRandomFolder(testName)
		defer os.RemoveAll(consulHome)
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
		readContentToUpload, err := io.ReadAll(&contentToUpload)
		assert.NoError(t, err)
		testStruct, cleanup := setup(t, testName, readContentToUpload)
		defer cleanup()
		mockDep := new(mocks.BusinessDependencies)
		mockDep.On(
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
		}, nil).On(
			"Chown", mock.AnythingOfType("string"), 1234, 123456,
		).Return(nil).On("Chmod", mock.AnythingOfType("string"), os.FileMode(0600)).Return(
			nil,
		)
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
		containerIP, err := testStruct.Container.ContainerIP(testStruct.CtxContainer)
		assert.NoError(t, err)
		aclTemplateData := struct {
			ZimbraHostname string
		}{ZimbraHostname: fmt.Sprintf("agent-%s", strings.ReplaceAll(containerIP, ".", "-"))}
		aclTemplate := template.Must(template.New("acl").Parse(command.AclPolicyTemplateText))
		aclRenderOut := bytes.Buffer{}
		assert.NoError(t, aclTemplate.Execute(&aclRenderOut, aclTemplateData))
		aclRenderBs, err := io.ReadAll(&aclRenderOut)
		assert.NoError(t, err)
		mockDep.On("LookupIP", containerIP).Return([]net.IP{net.IPv4(1, 1, 1, 1)}, nil)
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
				fmt.Sprintf("agent-%s", strings.ReplaceAll(containerIP, ".", "-")),
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
				fmt.Sprintf("agent-%s", strings.ReplaceAll(containerIP, ".", "-")),
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
		localConfig, err := carbonio.LoadLocalConfig(testStruct.FakeLocalConfig.Name())
		if err != nil {
			t.Error(err)
		}
		mockLdapHandler := new(mocks5.LdapHandler)
		mockLdapHandler.On("CheckServerAvailability", true).
			Return(nil)
		mockDep.
			On("SystemdUnitHandler").
			Return(unitManager, nil).
			On("LdapHandler", mock.Anything).
			Return(carbonio.CreateNewHandler(localConfig))
		s := &Setup{
			ConsulHome:        consulHome,
			LocalConfigPath:   testStruct.FakeLocalConfig.Name(),
			ConsulFileConfig:  mutableConfiguration.Name(),
			ClusterCredential: testStruct.ClusterCredentialDownload.Name(),
			MutableConfigFile: mutableConfig.Name(),
			Password:          "password",
			BindAddress:       "192.168.1.1",
		}
		_, err = s.setup(mockDep)
		assert.EqualError(t, err, fmt.Sprintf("unable to enable %s unit: %s", unitName, errorCause))
	})

	t.Run("Should properly run without errors", func(t *testing.T) {
		testName := "Should properly run without errors"
		contentToUpload := bytes.Buffer{}
		writer, err := credentialsEncrypter.NewWriter(&contentToUpload, []byte("password"))
		assert.NoError(t, err)
		mutableConfiguration := test.GenerateRandomFile(testName)
		defer os.Remove(mutableConfiguration.Name())
		mutableConfig := test.GenerateRandomFile(testName)
		defer os.Remove(mutableConfig.Name())
		consulHome := test.GenerateRandomFolder(testName)
		defer os.RemoveAll(consulHome)
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
		readContentToUpload, err := io.ReadAll(&contentToUpload)
		assert.NoError(t, err)
		testStruct, cleanup := setup(t, testName, readContentToUpload)
		defer cleanup()
		mockDep := new(mocks.BusinessDependencies)
		mockDep.On(
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
		}, nil).On(
			"Chown", mock.AnythingOfType("string"), 1234, 123456,
		).Return(nil).On("Chmod", mock.AnythingOfType("string"), os.FileMode(0600)).Return(
			nil,
		)
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
		containerIP, err := testStruct.Container.ContainerIP(testStruct.CtxContainer)
		assert.NoError(t, err)
		aclTemplateData := struct {
			ZimbraHostname string
		}{ZimbraHostname: fmt.Sprintf("agent-%s", strings.ReplaceAll(containerIP, ".", "-"))}
		aclTemplate := template.Must(template.New("acl").Parse(command.AclPolicyTemplateText))
		aclRenderOut := bytes.Buffer{}
		assert.NoError(t, aclTemplate.Execute(&aclRenderOut, aclTemplateData))
		aclRenderBs, err := io.ReadAll(&aclRenderOut)
		assert.NoError(t, err)
		mockDep.On("LookupIP", containerIP).Return([]net.IP{net.IPv4(1, 1, 1, 1)}, nil)
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
				fmt.Sprintf("agent-%s", strings.ReplaceAll(containerIP, ".", "-")),
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
				fmt.Sprintf("agent-%s", strings.ReplaceAll(containerIP, ".", "-")),
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
		localConfig, err := carbonio.LoadLocalConfig(testStruct.FakeLocalConfig.Name())
		if err != nil {
			t.Error(err)
		}
		mockDep.On("SystemdUnitHandler").Return(unitManager, nil)
		mockLdapHandler := new(mocks5.LdapHandler)
		mockLdapHandler.On("CheckServerAvailability", true).
			Return(nil)
		mockDep.
			On("SystemdUnitHandler").
			Return(unitManager, nil).
			On("LdapHandler", mock.Anything).
			Return(carbonio.CreateNewHandler(localConfig))
		s := &Setup{
			LocalConfigPath:   testStruct.FakeLocalConfig.Name(),
			ConsulFileConfig:  mutableConfiguration.Name(),
			ClusterCredential: testStruct.ClusterCredentialDownload.Name(),
			MutableConfigFile: mutableConfig.Name(),
			BindAddress:       "192.168.1.1",
			Password:          "password",
			ConsulHome:        consulHome,
		}
		_, err = s.setup(mockDep)
		assert.NoError(t, err)
		mutableConfigContent, err := os.ReadFile(mutableConfig.Name())
		assert.NoError(t, err)
		assert.Equal(t, `{
  "bind_addr": "192.168.1.1"
}`, string(mutableConfigContent))
	})
}
