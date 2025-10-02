// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package carbonio

import (
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-ldap/ldap"
	"github.com/pkg/errors"
)

const (
	LdapSeverBaseDn            = "cn=servers,cn=zimbra"
	LdapConfigBaseDn           = "cn=config,cn=zimbra"
	ServiceDiscoverServiceName = "service-discover"
	AttrServiceEnabled         = "zimbraServiceEnabled"
	AttrServiceHostname        = "zimbraServiceHostname"
	AttrCarbonioCredentials    = "carbonioMeshCredentials" // #nosec
)

type LdapHandler interface {
	AddService(server string, service string) error
	RemoveService(server string, service string) error
	QueryAllServersWithService(service string) ([]string, error)
	CheckServerAvailability(write bool) error
	UploadBinary(reader io.Reader, dn string, attribute string) error
	DownloadBinary(dn string, attribute string) ([]byte, error)
}

type ldapConnInterface interface {
	Add(request *ldap.AddRequest) error
	Del(request *ldap.DelRequest) error
	Bind(username, password string) error
	Modify(modifyRequest *ldap.ModifyRequest) error
	Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
	Close()
}

type ldapCredentials struct {
	MasterUrls  []string
	ReplicaUrls []string
	Username    string
	Password    string
}

type ldapContext struct {
	Credentials ldapCredentials
	Connect     func(url string) (ldapConnInterface, error)
}

func (l *ldapContext) UploadBinary(reader io.Reader, baseDN, attribute string) error {
	connection, err := connect(l, true)
	if err != nil {
		return err
	}
	defer connection.Close()

	content, err := io.ReadAll(reader)
	encodedContent := base64.StdEncoding.EncodeToString(content)

	if err != nil {
		return err
	}

	addRequest := ldap.NewModifyRequest(baseDN, []ldap.Control{})
	addRequest.Replace(attribute, []string{encodedContent})

	return connection.Modify(addRequest)
}

func (l *ldapContext) DownloadBinary(baseDN, attribute string) ([]byte, error) {
	connection, err := connect(l, false)
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	searchRequest := ldap.NewSearchRequest(
		baseDN,
		ldap.ScopeWholeSubtree,
		ldap.ScopeBaseObject,
		1,
		600,
		false,
		"("+attribute+"=*)",
		[]string{
			attribute,
		},
		[]ldap.Control{},
	)

	result, err := connection.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	if len(result.Entries) == 0 || len(result.Entries) > 1 {
		return nil, errors.Errorf("expected 1 ldap result but instead got %d", len(result.Entries))
	}

	entry := result.Entries[0]

	encodedContent := entry.GetAttributeValue(attribute)

	return base64.StdEncoding.DecodeString(encodedContent)
}

// CreateNewHandler Returns a new context to execute ldap queries.
func CreateNewHandler(localConfig LocalConfig) LdapHandler {
	return &ldapContext{
		readLdapCredentials(localConfig),
		standardLdapConnection(),
	}
}

func standardLdapConnection() func(url string) (ldapConnInterface, error) {
	return func(url string) (ldapConnInterface, error) {
		return ldap.DialURL(url)
	}
}

// CheckServerAvailability Returns an error if the server is not available.
func (l *ldapContext) CheckServerAvailability(write bool) error {
	connection, err := connect(l, write)
	if err != nil {
		return err
	}

	connection.Close()

	return nil
}

// AddService Adds to the provided server the service.
func (l *ldapContext) AddService(server, service string) error {
	return modifyEnabledServices(l, server, service, changeAdd)
}

// RemoveService Removes from the provided server the service.
func (l *ldapContext) RemoveService(server, service string) error {
	return modifyEnabledServices(l, server, service, changeRemove)
}

// QueryAllServersWithService Returns an array of all servers with the provided service.
func (l *ldapContext) QueryAllServersWithService(service string) ([]string, error) {
	connection, err := connect(l, false)
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	result, err := connection.Search(&ldap.SearchRequest{
		Scope:  ldap.ScopeSingleLevel,
		BaseDN: LdapSeverBaseDn,
		Filter: "(" + AttrServiceEnabled + "=" + service + ")",
		Attributes: []string{
			AttrServiceEnabled,
			AttrServiceHostname,
		},
	})
	if err != nil {
		return nil, err
	}

	var servers = make([]string, 0)
	for _, entry := range result.Entries {
		servers = append(servers, entry.GetAttributeValue(AttrServiceHostname))
	}

	return servers, nil
}

func readLdapCredentials(localConfig LocalConfig) ldapCredentials {
	return ldapCredentials{
		localConfig.Values(LocalConfigLdapMasterURL),
		localConfig.Values(LocalConfigLdapURL),
		localConfig.Value(LocalConfigLdapUserDn),
		localConfig.Value(LocalConfigLdapPassword),
	}
}

// ConnectURL creates a connection to the LDAP server.
// It checks that the ldap url contains within the ldaps scheme.
// If found it performs a TLS connection otherwise the connection goes in clear.
// It also implements a new connection timeout + retry 3 times otherwise it should be overkill to use default OS timeout.
func (ctx *ldapContext) ConnectURL(rawurl string) (ldapConnInterface, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}

	hostPort := u.Host
	useTLS := u.Scheme == "ldaps"

	// Set timeout for connection
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var conn net.Conn
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if useTLS {
			tlsConfig := &tls.Config{
				InsecureSkipVerify: true,
				ServerName:         strings.Split(u.Host, ":")[0],
			}
			conn, lastErr = tls.DialWithDialer(dialer, "tcp", hostPort, tlsConfig)
		} else {
			conn, lastErr = dialer.Dial("tcp", hostPort)
		}
		if lastErr == nil {
			break
		}
		// Wait briefly before retrying (optional)
		time.Sleep(200 * time.Millisecond)
	}

	if conn == nil {
		return nil, lastErr
	}

	ldapConn := ldap.NewConn(conn, useTLS)
	ldapConn.Start()

	return ldapConn, nil
}

func connect(context *ldapContext, writeAccess bool) (ldapConnInterface, error) {
	var connection ldapConnInterface

	var err error

	urls := context.Credentials.MasterUrls
	if !writeAccess {
		// we want to query masters before replicas
		// to get a more consistent view
		urls = append(urls, context.Credentials.ReplicaUrls...)
	}

	var lastErr error

	for _, url := range urls {
		connection, err = context.ConnectURL(url)
		if err == nil {
			break
		}

		lastErr = err
	}

	if connection == nil {
		if lastErr == nil {
			return nil, errors.New("no ldap defined in localconfig")
		}

		return nil, lastErr
	}

	err = connection.Bind(context.Credentials.Username, context.Credentials.Password)
	if err != nil {
		connection.Close()

		return nil, err
	}

	return connection, nil
}

const (
	changeAdd = iota
	changeRemove
)

type operationType = uint8

func modifyEnabledServices(context *ldapContext, server, service string, change operationType) error {
	connection, err := connect(context, true)
	if err != nil {
		return err
	}
	defer connection.Close()

	result, err := connection.Search(&ldap.SearchRequest{
		Scope:  ldap.ScopeSingleLevel,
		BaseDN: LdapSeverBaseDn,
		Filter: "(" + AttrServiceHostname + "=" + server + ")",
		Attributes: []string{
			"dn",
			AttrServiceEnabled,
		},
	})
	if err != nil {
		return err
	}

	if len(result.Entries) == 0 {
		return errors.New("server '" + server + "' not found on LDAP")
	}

	// check if already exist, other modify will fail
	attributeExist := false

	for _, serviceEnabled := range result.Entries[0].GetAttributeValues(AttrServiceEnabled) {
		if serviceEnabled == service {
			attributeExist = true

			break
		}
	}

	if attributeExist && change == changeAdd {
		return nil
	}

	if !attributeExist && change == changeRemove {
		return nil
	}

	request := ldap.ModifyRequest{
		DN: result.Entries[0].DN,
	}

	switch change {
	case changeAdd:
		request.Add(AttrServiceEnabled, []string{service})
	case changeRemove:
		request.Delete(AttrServiceEnabled, []string{service})
	default:
		panic("Invalid LDAP change")
	}

	err = connection.Modify(&request)
	if err != nil {
		return err
	}

	return nil
}
