// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	mocks22 "github.com/zextras/service-discover/pkg/exec/mocks"
	"os"
	"os/user"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/zextras/service-discover/pkg/carbonio"
	mocks2 "github.com/zextras/service-discover/pkg/carbonio/mocks"
	"github.com/zextras/service-discover/pkg/command/setup/mocks"
	"github.com/zextras/service-discover/test"
)

func TestSetup_isFirstInstance(t *testing.T) {
	t.Parallel()

	type testData struct {
		localConfigPath        string
		clusterCredentialsPath string
	}

	setup := func(t *testing.T, name string) (*testData, func()) {
		localConfig := test.GenerateRandomFile(name)
		clusterCredentials := test.GenerateRandomFile(name)
		assert.NoError(t, os.WriteFile(
			localConfig.Name(),
			test.GenerateLocalConfig(
				t,
				"mail.example.com",
				"ldap://mail.example.com:389",
				"ldap://mail.example.com:389",
				test.DefaultLdapUserDN,
				"password",
			),
			os.FileMode(0644),
		))

		return &testData{
				localConfigPath:        localConfig.Name(),
				clusterCredentialsPath: clusterCredentials.Name(),
			}, func() {
				err := os.Remove(localConfig.Name())
				if err != nil {
					t.Fatal(err)
				}

				err = os.Remove(clusterCredentials.Name())
				if err != nil {
					t.Fatal(err)
				}
			}
	}

	t.Run("Should give first instance", func(t *testing.T) {
		testData, cleanup := setup(t, "Should give first instance")
		defer cleanup()

		s := &Setup{
			ConsulConfigDir:   "",
			ConsulHome:        "",
			LocalConfigPath:   testData.localConfigPath,
			ConsulData:        "",
			ConsulFileConfig:  "",
			ClusterCredential: "",
			MutableConfigFile: "",
		}
		mockDep := new(mocks.BusinessDependencies)
		mockLdap := new(mocks2.LdapHandler)
		mockLdap.On("QueryAllServersWithService", carbonio.ServiceDiscoverServiceName).
			Return([]string{}, nil)
		mockDep.On("LdapHandler", mock.Anything).Return(mockLdap)
		got, err := s.isFirstInstance(mockDep)
		assert.NoError(t, err)
		assert.Equal(t, true, got)
	})

	t.Run("Should return not first instance if there are members in service-discover ldap", func(t *testing.T) {
		testData, cleanup := setup(t, "Should return not first instance if there are members in service-discover ldap")
		defer cleanup()

		s := &Setup{
			ConsulConfigDir:   "",
			ConsulHome:        "",
			LocalConfigPath:   testData.localConfigPath,
			ConsulData:        "",
			ConsulFileConfig:  "",
			ClusterCredential: "",
			MutableConfigFile: "",
		}
		mockDep := new(mocks.BusinessDependencies)
		mockLdap := new(mocks2.LdapHandler)
		mockLdap.On("QueryAllServersWithService", carbonio.ServiceDiscoverServiceName).
			Return([]string{"mail2.example.com"}, nil)
		mockDep.On("LdapHandler", mock.Anything).Return(mockLdap)
		got, err := s.isFirstInstance(mockDep)
		assert.NoError(t, err)
		assert.Equal(t, false, got)
	})

	t.Run("Should return not first instance if cluster credentials file is present", func(t *testing.T) {
		testData, cleanup := setup(t, "Should return not first instance if cluster credentials file is present")
		defer cleanup()

		s := &Setup{
			ConsulConfigDir:   "",
			ConsulHome:        "",
			LocalConfigPath:   testData.localConfigPath,
			ConsulData:        "",
			ConsulFileConfig:  "",
			ClusterCredential: testData.clusterCredentialsPath,
			MutableConfigFile: "",
		}
		mockDep := new(mocks.BusinessDependencies)
		mockLdap := new(mocks2.LdapHandler)
		mockLdap.On("QueryAllServersWithService", carbonio.ServiceDiscoverServiceName).
			Return([]string{"mail2.example.com"}, nil)
		mockDep.On("LdapHandler", mock.Anything).Return(mockLdap)
		got, err := s.isFirstInstance(mockDep)
		assert.NoError(t, err)
		assert.Equal(t, false, got)
	})
}

func TestSetup_generateCertificateAndConfig_TLS(t *testing.T) {
	consulHome := test.GenerateRandomFolder(t.Name())
	defer func(name string) {
		os.Remove(name)
	}(consulHome)
	setup := Setup{
		ConsulConfigDir:   "",
		ConsulHome:        consulHome,
		LocalConfigPath:   "",
		ConsulData:        "",
		ConsulFileConfig:  "",
		ClusterCredential: "",
		MutableConfigFile: "",
		Password:          "",
		BindAddress:       "",
		FirstInstance:     false,
	}
	mockDep := new(mocks.BusinessDependencies)
	tlsCommand := new(mocks22.Cmd)
	// TODO: mocking so many calls is a smell to me, think about re-organizing mocks maybe
	mockDep.On("CreateCommand", "/usr/bin/consul", "tls", "cert", "create", "-days=10950", "-server").Return(tlsCommand)
	tlsCommand.On("Output").Return([]uint8{}, nil)
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

	config, err := setup.generateCertificateAndConfig(mockDep, "localhost", "gossipKey")
	assert.NoError(t, err)
	assert.Equal(t, consulHome+"/consul-agent-ca.pem", config.TLS.Defaults.CaFile)
	assert.Equal(t, consulHome+"/dc1-server-consul-0-key.pem", config.TLS.Defaults.KeyFile)
	assert.Equal(t, consulHome+"/dc1-server-consul-0.pem", config.TLS.Defaults.CertFile)
	assert.Equal(t, true, config.TLS.Defaults.VerifyIncoming)
	assert.Equal(t, true, config.TLS.Defaults.VerifyOutgoing)
	assert.Equal(t, true, config.TLS.InternalRPC.VerifyServerHostname)
}
