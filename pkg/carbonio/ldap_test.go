// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package carbonio

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	"github.com/go-ldap/ldap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/testcontainers/testcontainers-go"
	"github.com/zextras/service-discover/test"
)

type MockLdapConnection struct {
	mock.Mock
}

func (mock *MockLdapConnection) Add(request *ldap.AddRequest) error {
	panic("should not be called")
}

func (mock *MockLdapConnection) Del(request *ldap.DelRequest) error {
	panic("should not be called")
}

func (mock *MockLdapConnection) Bind(username, password string) error {
	args := mock.Called(username, password)
	return args.Error(0)
}

func (mock *MockLdapConnection) Close() {
	mock.Called()
}

func (mock *MockLdapConnection) Modify(modifyRequest *ldap.ModifyRequest) error {
	args := mock.Called(modifyRequest)
	return args.Error(0)
}

func (mock *MockLdapConnection) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	args := mock.Called(searchRequest)
	return args.Get(0).(*ldap.SearchResult), args.Error(1)
}

func Test_connect(t *testing.T) {
	t.Run("connect without fallback", func(t *testing.T) {
		mockLdapConnection := new(MockLdapConnection)
		mockLdapConnection.On("Bind", "username", "password").Return(nil)

		got, err := connect(
			&ldapContext{
				Credentials: ldapCredentials{
					MasterUrls:  []string{"ldap://example.com:123"},
					ReplicaUrls: []string{"never use me"},
					Username:    "username",
					Password:    "password",
				},
				Connect: func(url string) (ldapConnInterface, error) {
					if url == "ldap://example.com:123" {
						return mockLdapConnection, nil
					} else {
						return nil, errors.New("invalid")
					}
				},
			},
			true,
		)
		assert.Nil(t, err)
		assert.Same(t, mockLdapConnection, got)
		mockLdapConnection.AssertCalled(t, "Bind", "username", "password")
	})
}

func TestEnableDisableService(t *testing.T) {
	t.Run("replica is not used for writes", func(t *testing.T) {
		handler := ldapContext{
			Credentials: ldapCredentials{
				MasterUrls:  []string{"ldap://example.com:123"},
				ReplicaUrls: []string{"never use me"},
				Username:    "username",
				Password:    "password",
			},
			Connect: func(url string) (ldapConnInterface, error) {
				if url == "ldap://example.com:123" {
					return nil, errors.New("master connection failed")
				} else {
					assert.Fail(t, "Replica must not be used")
					return nil, errors.New("replica must not be used")
				}
			},
		}

		err := handler.RemoveService(
			"server",
			"service",
		)
		assert.NotNil(t, err)
		assert.Equal(t, "master connection failed", err.Error())
	})

	t.Run("service added", func(t *testing.T) {
		mockLdapConnection := new(MockLdapConnection)
		mockLdapConnection.On("Close").Return()
		mockLdapConnection.On("Bind", "username", "password").Return(nil)

		searchQuery := ldap.SearchRequest{
			Scope:  ldap.ScopeSingleLevel,
			BaseDN: "cn=servers,cn=zimbra",
			Filter: "(zimbraServiceHostname=server-hostname)",
			Attributes: []string{
				"dn",
				"zimbraServiceEnabled",
			},
		}
		searchResult := ldap.SearchResult{
			Entries: []*ldap.Entry{{
				DN: "server-dn",
				Attributes: []*ldap.EntryAttribute{
					{
						Name:   "zimbraServiceEnabled",
						Values: []string{"mailbox"},
					},
				},
			}},
		}
		mockLdapConnection.On("Search", &searchQuery).Return(&searchResult, nil)

		expectedModifyRequest := ldap.ModifyRequest{
			DN: "server-dn",
		}
		expectedModifyRequest.Add("zimbraServiceEnabled", []string{"service"})
		mockLdapConnection.On("Modify", &expectedModifyRequest).Return(nil)

		handler := ldapContext{
			Credentials: ldapCredentials{
				MasterUrls:  []string{"ldap://example.com:123"},
				ReplicaUrls: []string{"never use me"},
				Username:    "username",
				Password:    "password",
			},
			Connect: func(url string) (ldapConnInterface, error) {
				if url == "ldap://example.com:123" {
					return mockLdapConnection, nil
				} else {
					assert.Fail(t, "Replica must not be used")
					return nil, errors.New("replica must noe be used")
				}
			},
		}

		err := handler.AddService(
			"server-hostname",
			"service",
		)
		assert.Nil(t, err)
		mockLdapConnection.AssertNumberOfCalls(t, "Modify", 1)
	})

	t.Run("service removed", func(t *testing.T) {
		mockLdapConnection := new(MockLdapConnection)
		mockLdapConnection.On("Close").Return()
		mockLdapConnection.On("Bind", "username", "password").Return(nil)

		searchQuery := ldap.SearchRequest{
			Scope:  ldap.ScopeSingleLevel,
			BaseDN: "cn=servers,cn=zimbra",
			Filter: "(zimbraServiceHostname=server-hostname)",
			Attributes: []string{
				"dn",
				"zimbraServiceEnabled",
			},
		}
		searchResult := ldap.SearchResult{
			Entries: []*ldap.Entry{{
				DN: "server-dn",
				Attributes: []*ldap.EntryAttribute{
					{
						Name:   "zimbraServiceEnabled",
						Values: []string{"mailbox", "service"},
					},
				},
			}},
		}
		mockLdapConnection.On("Search", &searchQuery).Return(&searchResult, nil)

		expectedModifyRequest := ldap.ModifyRequest{
			DN: "server-dn",
		}
		expectedModifyRequest.Delete("zimbraServiceEnabled", []string{"service"})
		mockLdapConnection.On("Modify", &expectedModifyRequest).Return(nil)

		handler := ldapContext{
			Credentials: ldapCredentials{
				MasterUrls:  []string{"ldap://example.com:123"},
				ReplicaUrls: []string{"never use me"},
				Username:    "username",
				Password:    "password",
			},
			Connect: func(url string) (ldapConnInterface, error) {
				if url == "ldap://example.com:123" {
					return mockLdapConnection, nil
				} else {
					assert.Fail(t, "Replica must not be used")
					return nil, errors.New("replica must noe be used")
				}
			},
		}

		err := handler.RemoveService(
			"server-hostname",
			"service",
		)
		assert.Nil(t, err)
		mockLdapConnection.AssertNumberOfCalls(t, "Modify", 1)
	})
}

func TestQueryAllServiceDiscoverServers(t *testing.T) {
	t.Run("query all servers", func(t *testing.T) {
		mockLdapConnection := new(MockLdapConnection)
		mockLdapConnection.On("Close").Return()
		mockLdapConnection.On("Bind", "username", "password").Return(nil)

		searchQuery := ldap.SearchRequest{
			Scope:  ldap.ScopeSingleLevel,
			BaseDN: "cn=servers,cn=zimbra",
			Filter: "(zimbraServiceEnabled=service)",
			Attributes: []string{
				"zimbraServiceEnabled",
				"zimbraServiceHostname",
			},
		}
		searchResult := ldap.SearchResult{
			Entries: []*ldap.Entry{{
				DN: "server-dn",
				Attributes: []*ldap.EntryAttribute{
					{
						Name:   "zimbraServiceHostname",
						Values: []string{"server-hostname"},
					},
				},
			}},
		}
		mockLdapConnection.On("Search", &searchQuery).Return(&searchResult, nil)

		expectedModifyRequest := ldap.ModifyRequest{
			DN: "server-dn",
		}
		expectedModifyRequest.Delete("zimbraServiceEnabled", []string{"service"})
		mockLdapConnection.On("Modify", &expectedModifyRequest).Return(nil)

		handler := ldapContext{
			Credentials: ldapCredentials{
				MasterUrls:  []string{"ldap://example.com:123"},
				ReplicaUrls: []string{"never use me"},
				Username:    "username",
				Password:    "password",
			},
			Connect: func(url string) (ldapConnInterface, error) {
				if url == "ldap://example.com:123" {
					return mockLdapConnection, nil
				} else {
					assert.Fail(t, "Replica must not be used")
					return nil, errors.New("replica must noe be used")
				}
			},
		}

		got, err := handler.QueryAllServersWithService(
			"service",
		)
		assert.Nil(t, err)
		assert.EqualValues(t, []string{"server-hostname"}, got)
	})

	t.Run("both master and replica fails", func(t *testing.T) {
		handler := ldapContext{
			Credentials: ldapCredentials{
				MasterUrls:  []string{"ldap://example.com:123"},
				ReplicaUrls: []string{"never use me"},
				Username:    "username",
				Password:    "password",
			},
			Connect: func(url string) (ldapConnInterface, error) {
				if url == "ldap://example.com:123" {
					return nil, errors.New("connection failed")
				} else {
					return nil, errors.New("replica connection failed")
				}
			},
		}

		got, err := handler.QueryAllServersWithService(
			"service",
		)
		assert.Nil(t, got)
		assert.NotNil(t, err)
		assert.Equal(t, "replica connection failed", err.Error())
	})
}

func TestLDAPDownloadAndUploadCapabilities(t *testing.T) {
	testDn := "cn=config,cn=zimbra"
	testAttribute := "carbonioMeshCredentials"

	t.Run("should download content from LDAP", func(t *testing.T) {
		expectedContent := make([]byte, 2048000) // 2 MB random byte array to simulate random binary content
		_, err := rand.Read(expectedContent)
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

		ldapHandler := ldapContext{
			Credentials: ldapCredentials{
				MasterUrls:  []string{masterUrl},
				ReplicaUrls: []string{},
				Username:    "uid=zimbra,cn=admins,cn=zimbra",
				Password:    "password",
			},
			Connect: standardLdapConnection(),
		}

		connection, err := connect(&ldapHandler, true)
		assert.NoError(t, err)

		expectedEncoded := base64.StdEncoding.EncodeToString(expectedContent)
		modRequest := ldap.NewModifyRequest(testDn, []ldap.Control{})
		modRequest.Replace(testAttribute, []string{expectedEncoded})
		err = connection.Modify(modRequest)
		assert.NoError(t, err)

		downloadedBinary, err := ldapHandler.DownloadBinary(testDn, testAttribute)
		assert.NoError(t, err)

		assert.Equal(t, expectedContent, downloadedBinary, "The two byte arrays are no the same")
	})

	t.Run("should upload content from LDAP", func(t *testing.T) {
		expectedContent := make([]byte, 2048000) // 2 MB random byte array to simulate random binary content
		_, err := rand.Read(expectedContent)
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

		ldapHandler := ldapContext{
			Credentials: ldapCredentials{
				MasterUrls:  []string{masterUrl},
				ReplicaUrls: []string{},
				Username:    "uid=zimbra,cn=admins,cn=zimbra",
				Password:    "password",
			},
			Connect: standardLdapConnection(),
		}

		err = ldapHandler.UploadBinary(bytes.NewBuffer(expectedContent), testDn, testAttribute)
		assert.NoError(t, err)

		connection, err := connect(&ldapHandler, false)
		assert.NoError(t, err)
		defer connection.Close()

		searchRequest := ldap.NewSearchRequest(
			testDn,
			ldap.ScopeWholeSubtree,
			ldap.ScopeBaseObject,
			1,
			600,
			false,
			"("+testAttribute+"=*)",
			[]string{
				testAttribute,
			},
			[]ldap.Control{},
		)

		result, err := connection.Search(searchRequest)
		assert.NoError(t, err)

		assert.Equal(t, 1, len(result.Entries), "Expected exactly 1 result from ldap")

		entry := result.Entries[0]

		encodedContent := entry.GetAttributeValue(testAttribute)
		decodedContent, err := base64.StdEncoding.DecodeString(encodedContent)
		assert.NoError(t, err)
		assert.Equal(t, expectedContent, decodedContent, "The downloaded content doesn't match the uploaded one")
	})
}
