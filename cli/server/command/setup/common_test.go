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
	"os"
	"testing"

	"github.com/Zextras/service-discover/cli/lib/carbonio"
	mocks2 "github.com/Zextras/service-discover/cli/lib/carbonio/mocks"
	"github.com/Zextras/service-discover/cli/lib/test"
	"github.com/Zextras/service-discover/cli/server/command/setup/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
