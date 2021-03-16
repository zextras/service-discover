package zimbra

import (
	"errors"
	"github.com/go-ldap/ldap/v3"
)

const (
	LdapSeverBaseDn            = "cn=servers,cn=zimbra"
	ServiceDiscoverServiceName = "service-discover"
	AttrServiceEnabled         = "zimbraServiceEnabled"
	AttrServiceHostname        = "zimbraServiceHostname"
)

type LdapHandler interface {
	AddService(server string, service string) error
	RemoveService(server string, service string) error
	QueryAllServersWithService(service string) ([]string, error)
	CheckServerAvailability(write bool) error
}

type ldapConnInterface interface {
	Bind(username, password string) error
	Modify(modifyRequest *ldap.ModifyRequest) error
	Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
	Close()
}

type ldapContext struct {
	Credentials ldapCredentials
	Connect     func(url string) (ldapConnInterface, error)
}

type ldapCredentials struct {
	MasterUrl  string
	ReplicaUrl string
	Username   string
	Password   string
}

//CreateNewHandler Returns a new context to execute ldap queries
func CreateNewHandler(localConfig LocalConfig) LdapHandler {
	return &ldapContext{
		readLdapCredentials(localConfig),
		func(url string) (ldapConnInterface, error) {
			return ldap.DialURL(url)
		},
	}
}

//CheckServerAvailability Returns an error if the server is not available
func (context *ldapContext) CheckServerAvailability(write bool) error {
	connection, err := connect(context, write)
	if err != nil {
		return err
	}
	connection.Close()
	return nil
}

//AddServiceForLocalServer Adds to the provided server the service
func (context *ldapContext) AddService(server string, service string) error {
	return modifyEnabledServices(context, server, service, changeAdd)
}

//RemoveServiceForLocalServer Removes from the provided server the service
func (context *ldapContext) RemoveService(server string, service string) error {
	return modifyEnabledServices(context, server, service, changeRemove)
}

//QueryAllServersWithService Returns an array of all servers with the provided service
func (context *ldapContext) QueryAllServersWithService(service string) ([]string, error) {
	connection, err := connect(context, false)
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

	var servers []string
	for _, entry := range result.Entries {
		servers = append(servers, entry.GetAttributeValue(AttrServiceHostname))
	}

	return servers, nil
}

func readLdapCredentials(localConfig LocalConfig) ldapCredentials {
	return ldapCredentials{
		localConfig.Value(LocalConfigLdapMasterUrl),
		localConfig.Value(LocalConfigLdapUrl),
		localConfig.Value(LocalConfigLdapUserDn),
		localConfig.Value(LocalConfigLdapPassword),
	}
}

func connect(context *ldapContext, writeAccess bool) (ldapConnInterface, error) {
	var connection ldapConnInterface
	var err error

	connection, err = context.Connect(context.Credentials.MasterUrl)
	if err != nil {
		if writeAccess {
			//to write we need master
			return nil, err
		} else {
			connection, err = context.Connect(context.Credentials.ReplicaUrl)
			if err != nil {
				return nil, err
			}
		}
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

func modifyEnabledServices(context *ldapContext, server string, service string, change operationType) error {
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

	//check if already exist, other modify will fail
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
		break
	case changeRemove:
		request.Delete(AttrServiceEnabled, []string{service})
		break
	default:
		panic("Invalid LDAP change")
	}

	err = connection.Modify(&request)
	if err != nil {
		return err
	}

	return nil
}
