// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"testing"

	"github.com/go-ldap/ldap/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/testcontainers/testcontainers-go"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/carbonio/mocks"
	"github.com/zextras/service-discover/test"
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

func (m *mockedNetworkInterfaces) LookupIP(s string) ([]net.IP, error) {
	ret := m.Called(s)

	var r0 []net.IP
	if rf, ok := ret.Get(0).(func(string) []net.IP); ok {
		r0 = rf(s)
	} else if ret.Get(0) != nil {
		r0 = ret.Get(0).([]net.IP)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(s)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

func TestCheckValidBindingAddress(t *testing.T) {
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

		err := CheckValidBindingAddress(
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

		err := CheckValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"192.168.1.2", // random one, it doesn't really matter
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

		err := CheckValidBindingAddress(
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

		err := CheckValidBindingAddress(
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

		err := CheckValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"10.0.0.1/8",
		)
		assert.EqualError(t, err, "invalid binding address selected")
	})

	t.Run("Address resolution failure", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		mockDependencies.On("LookupIP", "address").Return([]net.IP{}, errors.New("random-failure"))
		err := CheckHostnameAddress(
			mockDependencies,
			"address",
		)
		assert.EqualError(t, err, "cannot resolve hostname 'address': random-failure")
	})

	t.Run("Address does not resolve", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		mockDependencies.On("LookupIP", "address").Return([]net.IP{}, nil)
		err := CheckHostnameAddress(
			mockDependencies,
			"address",
		)
		assert.EqualError(t, err, "cannot resolve hostname 'address'")
	})

	t.Run("Address resolve with localhost", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		mockDependencies.On("LookupIP", "address").Return(
			[]net.IP{net.IPv4(127, 0, 0, 1)},
			nil,
		)

		err := CheckHostnameAddress(
			mockDependencies,
			"address",
		)
		assert.EqualError(t, err, "hostname 'address' is resolving with loopback address, should resolve with LAN address")
	})

	t.Run("Address resolve with LAN", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		mockDependencies.On("LookupIP", "address").Return([]net.IP{net.IPv4(1, 1, 1, 1)}, nil)
		err := CheckHostnameAddress(
			mockDependencies,
			"address",
		)
		assert.NoError(t, err)
	})
}

func TestSetup_openClusterCredential(t *testing.T) {
	t.Parallel()

	t.Run("File doesn't exists", func(t *testing.T) {
		nonExistingFile := test.GenerateRandomFile("File doesn't exists")
		assert.NoError(t, os.Remove(nonExistingFile.Name()))
		_, err := OpenClusterCredential(nonExistingFile.Name())
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"cannot find Cluster credential in %s, please copy the file from the existing server or upload it to LDAP",
				nonExistingFile.Name(),
			),
		)
	})

	t.Run("File exists", func(t *testing.T) {
		existingFile := test.GenerateRandomFile("File exists")
		defer os.Remove(existingFile.Name())
		_, err := OpenClusterCredential(existingFile.Name())
		assert.NoError(t, err)
	})
}

func TestSaveBindAddressConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("Should check that the file is correctly written", func(t *testing.T) {
		actualResult := test.GenerateRandomFile("Should check that the file is correctly written")
		defer os.Remove(actualResult.Name())

		assert.NoError(t, SaveBindAddressConfiguration(actualResult.Name(), "127.0.0.1"))
		actualResultContent, err := os.ReadFile(actualResult.Name())
		assert.NoError(t, err)
		assert.Equal(t, `{
  "bind_addr": "127.0.0.1"
}`, string(actualResultContent))
	})

	t.Run("Doesn't write network mask", func(t *testing.T) {
		actualResult := test.GenerateRandomFile("Should check that the file is correctly written")
		defer os.Remove(actualResult.Name())

		assert.NoError(t, SaveBindAddressConfiguration(actualResult.Name(), "127.0.0.1/24"))
		actualResultContent, err := os.ReadFile(actualResult.Name())
		assert.NoError(t, err)
		assert.Equal(t, `{
  "bind_addr": "127.0.0.1"
}`, string(actualResultContent))
	})
}

func TestCredentialsFromAndToLDAP(t *testing.T) {
	t.Run("should upload the value to LDAP", func(t *testing.T) {
		uploadFile := test.GenerateRandomFile("fakeCredentials*.tar")
		defer func(name string) {
			if err := os.Remove(name); err != nil {
				t.Error(err)
			}
		}(uploadFile.Name())

		randomContent := make([]byte, 4096000) // 4 MB random byte array to simulate random binary content
		_, err := rand.Read(randomContent)
		assert.NoError(t, err)
		err = os.WriteFile(uploadFile.Name(), randomContent, 0777)
		assert.NoError(t, err)
		ldapContainer, containerCtx := test.SpinUpCarbonioLdap(t, test.PUBLIC_IMAGE_ADDRESS, test.LATEST_RELEASE)

		defer func(ldapContainer testcontainers.Container, ctx context.Context) {
			if err := ldapContainer.Terminate(ctx); err != nil {
				t.Error(err)
			}
		}(ldapContainer, containerCtx)

		ldapIp, err := ldapContainer.ContainerIP(containerCtx)
		assert.NoError(t, err)

		masterUrl := fmt.Sprintf("ldap://%s:%s", ldapIp, "389")

		mockedLocalConfig := new(mocks.LocalConfig)
		mockedLocalConfig.On("Values", carbonio.LocalConfigLdapMasterUrl).Return([]string{masterUrl}).On("Values", carbonio.LocalConfigLdapUrl).Return([]string{}).On("Value", carbonio.LocalConfigLdapUserDn).Return("uid=zimbra,cn=admins,cn=zimbra").On("Value", carbonio.LocalConfigLdapPassword).Return("password")

		ldapHandler := carbonio.CreateNewHandler(mockedLocalConfig)

		err = UploadCredentialsToLDAP(ldapHandler, uploadFile.Name())
		assert.NoError(t, err)

		// Try to download the content and check that it is the same
		ldapConnection, err := ldap.DialURL(masterUrl)
		assert.NoError(t, err)
		err = ldapConnection.Bind("uid=zimbra,cn=admins,cn=zimbra", "password")
		assert.NoError(t, err)

		result, err := ldapConnection.Search(ldap.NewSearchRequest(
			carbonio.LdapConfigBaseDn,
			ldap.ScopeWholeSubtree,
			ldap.ScopeBaseObject,
			1,
			600,
			false,
			"("+carbonio.AttrCarbonioCredentials+"=*)",
			[]string{
				carbonio.AttrCarbonioCredentials,
			},
			[]ldap.Control{},
		))
		assert.NoError(t, err)
		assert.Equal(t, 1, len(result.Entries), "Expected exactly 1 result from ldap")

		entry := result.Entries[0]

		encodedContent := entry.GetAttributeValue(carbonio.AttrCarbonioCredentials)
		decodedContent, err := base64.StdEncoding.DecodeString(encodedContent)
		assert.NoError(t, err)
		assert.Equal(t, randomContent, decodedContent, "The downloaded content doesn't match the uploaded one")
	})

	t.Run("should download the value from LDAP", func(t *testing.T) {
		randomContent := make([]byte, 4096000) // 4 MB random byte array to simulate random binary content
		_, err := rand.Read(randomContent)
		assert.NoError(t, err)
		ldapContainer, containerCtx := test.SpinUpCarbonioLdap(t, test.PUBLIC_IMAGE_ADDRESS, test.LATEST_RELEASE)

		defer func(ldapContainer testcontainers.Container, ctx context.Context) {
			if err := ldapContainer.Terminate(ctx); err != nil {
				t.Error(err)
			}
		}(ldapContainer, containerCtx)

		ldapIp, err := ldapContainer.ContainerIP(containerCtx)
		assert.NoError(t, err)

		masterUrl := fmt.Sprintf("ldap://%s:%s", ldapIp, "389")
		// Try to download the content and check that it is the same
		ldapConnection, err := ldap.DialURL(masterUrl)
		assert.NoError(t, err)
		err = ldapConnection.Bind("uid=zimbra,cn=admins,cn=zimbra", "password")
		assert.NoError(t, err)

		expectedEncoded := base64.StdEncoding.EncodeToString(randomContent)
		modRequest := ldap.NewModifyRequest(carbonio.LdapConfigBaseDn, []ldap.Control{})
		modRequest.Replace(carbonio.AttrCarbonioCredentials, []string{expectedEncoded})
		err = ldapConnection.Modify(modRequest)
		assert.NoError(t, err)

		mockedLocalConfig := new(mocks.LocalConfig)
		mockedLocalConfig.On("Values", carbonio.LocalConfigLdapMasterUrl).Return([]string{masterUrl}).On("Values", carbonio.LocalConfigLdapUrl).Return([]string{}).On("Value", carbonio.LocalConfigLdapUserDn).Return("uid=zimbra,cn=admins,cn=zimbra").On("Value", carbonio.LocalConfigLdapPassword).Return("password")

		ldapHandler := carbonio.CreateNewHandler(mockedLocalConfig)

		downloadedContent := test.GenerateRandomFile("testDownloadLdap*.tar")
		defer func(name string) {
			if err := os.Remove(name); err != nil {
				t.Error(err)
			}
		}(downloadedContent.Name())

		err = DownloadCredentialsFromLDAP(ldapHandler, downloadedContent.Name())
		assert.NoError(t, err)

		gotContent, err := os.ReadFile(downloadedContent.Name())
		assert.NoError(t, err)

		assert.Equal(t, randomContent, gotContent, "The downloaded content doesn't match the desired one")
	})
}
