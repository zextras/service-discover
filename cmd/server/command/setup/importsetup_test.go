// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net"
	"os"
	native_exec "os/exec"
	"os/user"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/testcontainers/testcontainers-go"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	mocks2 "github.com/zextras/service-discover/pkg/command/setup/mocks"
	"github.com/zextras/service-discover/pkg/encrypter"
	mocks3 "github.com/zextras/service-discover/pkg/exec/mocks"
	mocks4 "github.com/zextras/service-discover/pkg/systemd/mocks"
	"github.com/zextras/service-discover/test"
)

// fakeCredentialsTar represents a valid credentials.tar.gpg file. It has
// been pasted as part of the codebase because golang doens't have a well
// defined way to point out test resources, so in order to avoid tests
// randomly failing I preferred pasting it here (it's still text after all
// luckly). You can read more here:
// https://web.archive.org/web/20230113095034/https://groups.google.com/g/Golang-Nuts/c/VPVlIiO5yXw
// Archive password: `assext`.
const fakeCredentialsTar = `-----BEGIN PGP MESSAGE-----

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
=80F+
-----END PGP MESSAGE-----`

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

func (f FakeFileStat) Sys() any {
	panic("implement me")
}

func TestSetup_importSetup(t *testing.T) {

	type setupOutput struct {
		FakeLocalConfig           *os.File
		ClusterCredentialDownload *os.File
		Container                 testcontainers.Container
		CtxContainer              context.Context
		consulConfigDir           string
		consulHome                string
		consulData                string
		consulFileConfig          string
		consulAclBootstrap        string
		consulCertificate         string
		consulCAKeyFile           string
		mutableConfigFile         string
	}

	defaultClusterCredentialsPassword := "assext"
	testingMode = true

	setup := func(t *testing.T, testName string, includeTar bool) (*setupOutput, func()) {
		var clusterCredentialsContent []byte
		if includeTar {
			clusterCredentialsContent = []byte(fakeCredentialsTar)
		}

		container, ctxContainer := test.SpinUpCarbonioLdap(t, test.PublicImageAddress, test.LatestRelease)

		containerIP, err := container.ContainerIP(ctxContainer)
		containerPort, err := container.MappedPort(ctxContainer, "1389")
		if err != nil {
			t.Error(err)
		}

		ldapUrl := "ldap://localhost:" + containerPort.Port()
		localConfigByte := test.GenerateLocalConfig(
			t,
			containerIP,
			ldapUrl,
			ldapUrl,
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

		connection, err := ldap.DialURL(ldapUrl, ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Minute}))
		if err != nil {
			t.Error(err)
		}

		//if err := connection.Bind(test.DefaultLdapUserDN, "password"); err != nil {
		//	t.Error(err)
		//}
		if err := connection.Bind(test.DefaultLdapUserDN, "password"); err != nil {
			t.Error(err)
		}

		if includeTar {
			encodedContent := base64.StdEncoding.EncodeToString(clusterCredentialsContent)
			modRequest := ldap.NewModifyRequest("cn=config,cn=zimbra", []ldap.Control{})
			modRequest.Replace("carbonioMeshCredentials", []string{encodedContent})
			err = connection.Modify(modRequest)
			assert.NoError(t, err)
		} else {
			t.Log("Skipping credentials upload since inclusion is not set")
		}

		addRequest := ldap.NewAddRequest(fmt.Sprintf("cn=%s,cn=servers,cn=zimbra", containerIP), []ldap.Control{})
		addRequest.Attribute("objectClass", []string{"zimbraServer"})
		addRequest.Attribute("zimbraId", []string{"27a46c9c-7bcb-46e0-a1c3-43b2ee3f3d8e"})
		addRequest.Attribute("zimbraServiceHostname", []string{containerIP})
		err = connection.Add(addRequest)
		assert.NoError(t, err)

		clusterCredentialDownloadFile := test.GenerateRandomFile(testName)
		consulConfigDir := test.GenerateRandomFolder(testName)
		consulHome := test.GenerateRandomFolder(testName)
		consulData := test.GenerateRandomFolder(testName)
		clusterFile := test.GenerateRandomFile(testName)
		consulAclBootstrap := test.GenerateRandomFile(testName)
		consulCertificate := test.GenerateRandomFile(testName)
		consulCAKeyFile := test.GenerateRandomFile(testName)
		mutableConfigFile := test.GenerateRandomFile(testName)

		// Cleanup function
		return &setupOutput{
				file,
				clusterCredentialDownloadFile,
				container,
				ctxContainer,
				consulConfigDir,
				consulHome,
				consulData,
				clusterFile.Name(),
				consulAclBootstrap.Name(),
				consulCertificate.Name(),
				consulCAKeyFile.Name(),
				mutableConfigFile.Name(),
			}, func() {
				if err := os.RemoveAll(consulConfigDir); err != nil {
					t.Error(err)
				}

				if err := os.RemoveAll(consulHome); err != nil {
					t.Error(err)
				}

				if err := os.RemoveAll(consulData); err != nil {
					t.Error(err)
				}

				if err := os.RemoveAll(clusterFile.Name()); err != nil {
					t.Error(err)
				}

				if err := os.RemoveAll(consulAclBootstrap.Name()); err != nil {
					t.Error(err)
				}

				if err := os.RemoveAll(consulCertificate.Name()); err != nil {
					t.Error(err)
				}

				if err := os.RemoveAll(consulCAKeyFile.Name()); err != nil {
					t.Error(err)
				}

				if err := os.RemoveAll(mutableConfigFile.Name()); err != nil {
					t.Error(err)
				}

				if err := container.Terminate(ctxContainer); err != nil {
					t.Error(err)
				}

				if err := os.Remove(file.Name()); err != nil {
					t.Error(err)
				}

				if err := os.Remove(clusterCredentialDownloadFile.Name()); err != nil {
					if !os.IsNotExist(err) {
						t.Error(err)
					}
				}
			}
	}

	t.Run("Cluster credentials is required", func(t *testing.T) {
		setupFiles, cleanup := setup(t, "Test cluster credentials is required", false)
		defer cleanup()

		containerIP, err := setupFiles.Container.ContainerIP(setupFiles.CtxContainer)
		assert.NoError(t, err)

		businessDep := new(mocks2.BusinessDependencies)
		setupNetwork(businessDep, containerIP)
		setupLdap(t, businessDep, setupFiles.FakeLocalConfig)
		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   setupFiles.FakeLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.ClusterCredentialDownload.Name(),
			MutableConfigFile: setupFiles.mutableConfigFile,
			BindAddress:       "127.0.0.1",
		}

		assert.NoError(t, os.Remove(setupFiles.ClusterCredentialDownload.Name()))

		_, err = s.importSetup(businessDep)
		assert.EqualError(
			t,
			err,
			"unable to download credentials from LDAP: unable to download data from ldap: expected 1 ldap result but instead got 0",
		)
	})

	t.Run("Returns error when using Wrong binding address", func(t *testing.T) {
		setupFiles, cleanup := setup(t, "Wrong binding address", true)
		defer cleanup()

		containerIP, err := setupFiles.Container.ContainerIP(setupFiles.CtxContainer)
		assert.NoError(t, err)

		businessDep := new(mocks2.BusinessDependencies)
		setupNetwork(businessDep, containerIP)

		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   setupFiles.FakeLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.ClusterCredentialDownload.Name(),
			MutableConfigFile: setupFiles.mutableConfigFile,
			Password:          defaultClusterCredentialsPassword,
		}
		s.BindAddress = "wrong_one"
		_, err = s.importSetup(businessDep)
		assert.EqualError(
			t,
			err,
			"invalid binding address selected",
		)
	})

	t.Run("Returns error when using wrong cluster credentials password", func(t *testing.T) {
		setupFiles, cleanup := setup(t, "Wrong cluster credentials password", true)
		defer cleanup()

		containerIP, err := setupFiles.Container.ContainerIP(setupFiles.CtxContainer)
		assert.NoError(t, err)

		businessDep := new(mocks2.BusinessDependencies)
		setupNetwork(businessDep, containerIP)
		setupLdap(t, businessDep, setupFiles.FakeLocalConfig)
		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   setupFiles.FakeLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.ClusterCredentialDownload.Name(),
			MutableConfigFile: setupFiles.mutableConfigFile,
			BindAddress:       "127.0.0.1",
			Password:          "not right one",
		}
		file, err := os.Create(setupFiles.ClusterCredentialDownload.Name())
		assert.NoError(t, err)
		tarWriter, err := encrypter.NewWriter(file, []byte("password"))
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
				setupFiles.ClusterCredentialDownload.Name(),
			),
		)
	})

	t.Run("Run with correct configuration and flags", func(t *testing.T) {
		setupFiles, cleanup := setup(t, "Run with correct configuration and flags", true)
		defer cleanup()

		containerIP, err := setupFiles.Container.ContainerIP(setupFiles.CtxContainer)
		assert.NoError(t, err)

		businessDep := new(mocks2.BusinessDependencies)
		setupNetwork(businessDep, containerIP)
		setupBusinessDeps(businessDep)

		s := &Setup{
			ConsulConfigDir:   setupFiles.consulConfigDir,
			ConsulHome:        setupFiles.consulHome,
			LocalConfigPath:   setupFiles.FakeLocalConfig.Name(),
			ConsulData:        setupFiles.consulData,
			ConsulFileConfig:  setupFiles.consulFileConfig,
			ClusterCredential: setupFiles.ClusterCredentialDownload.Name(),
			MutableConfigFile: setupFiles.mutableConfigFile,
			Password:          defaultClusterCredentialsPassword,
			BindAddress:       "127.0.0.1",
		}
		clusterCredential, err := os.Create(setupFiles.ClusterCredentialDownload.Name())
		assert.NoError(t, err)
		tarWriter, err := encrypter.NewWriter(clusterCredential, []byte("password"))
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
			command.ConsulACLBootstrap,
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
			fmt.Sprintf("server-%s", strings.ReplaceAll(containerIP, ".", "-")),
			"-rules",
			fmt.Sprintf(`{
   "node":{
      "server-%s":{
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
}`, strings.ReplaceAll(containerIP, ".", "-")),
		).Return(aclPolicyCreateMock, nil).
			On("CreateCommand",
				"/usr/bin/consul",
				"acl",
				"token",
				"create",
				"-policy-name",
				fmt.Sprintf("server-%s", strings.ReplaceAll(containerIP, ".", "-")),
				"-format",
				"json").
			Return(tokenCreateMock)

		var cleanups = make([]func(), 0)
		defer func() {
			for _, f := range cleanups {
				f()
			}
		}()
		setupLdap(t, businessDep, setupFiles.FakeLocalConfig)

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

func setupLdap(t *testing.T, businessDep *mocks2.BusinessDependencies, fakeLocalConfig *os.File) {
	localConfig, err := carbonio.LoadLocalConfig(fakeLocalConfig.Name())
	if err != nil {
		t.Error(err)
	}

	businessDep.On("LdapHandler", mock.Anything).Return(carbonio.CreateNewHandler(localConfig))
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
func setupNetwork(businessDep *mocks2.BusinessDependencies, containerIp string) {
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

	businessDep.On("LookupIP", containerIp).Return(
		[]net.IP{net.IPv4(1, 1, 1, 1)},
		nil,
	)
}
