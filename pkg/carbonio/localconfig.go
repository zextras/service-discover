// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package carbonio

import (
	"encoding/xml"
	"errors"
	"os"
	"strings"
)

const LocalConfigLdapMasterURL = "ldap_master_url"
const LocalConfigLdapURL = "ldap_url"
const LocalConfigLdapUserDn = "zimbra_ldap_userdn"
const LocalConfigLdapPassword = "zimbra_ldap_password" // #nosec
const LocalConfigServerHostname = "zimbra_server_hostname"
const LocalConfigPath = "/opt/zextras/conf/localconfig.xml"

// rawKey represents an entry in the Zimbra local config.
type rawKey struct {
	Text  string `xml:",chardata"`
	Name  string `xml:"name,attr"`
	Value string `xml:"value"`
}

// rawLocalConfig represent the whole Zimbra local config structure.
type rawLocalConfig struct {
	XMLName xml.Name `xml:"localconfig"`
	Text    string   `xml:",chardata"`
	Key     []rawKey `xml:"key"`
}

// LocalConfigEntry represent a possible value that a Zimbra local config can have.
type LocalConfigEntry struct {
	Text  string // Represents a possible description for that entry
	Value string // Represents the actual value for that entry
}

func loadLocalConfig(path string) (*rawLocalConfig, error) {
	carbonioLocalConfig := &rawLocalConfig{}
	localConfigBytes, err := os.ReadFile(path) // #nosec

	if err != nil {
		return nil, errors.New("unable to parse local config at: " + path)
	}

	err = xml.Unmarshal(localConfigBytes, carbonioLocalConfig)
	if err != nil {
		return nil, err
	}
	return carbonioLocalConfig, nil
}

// A LocalConfig represents a Zimbra configuration stored by Zimbra. Normally, this configuration is in form of an XML,
// and it is stored in localconfig.xml. That said, a local config can proved bot a value and an additional description
// for the desired key.
type LocalConfig interface {
	Value(key string) string
	Values(url string) []string
	Text(key string) string
}

// indexedLocalConfig is a Zimbra local config that has already been parsed. This provides a fast access since each
// entry is stored in a map.
type indexedLocalConfig struct {
	localConfigIndex map[string]*LocalConfigEntry
}

// LoadLocalConfig loads a Zimbra local configuration located at the desired path. The current behavior is to load the
// XML file and parse it storing all the values in RAM. This allows for faster retrieval during the program execution.
func LoadLocalConfig(path string) (LocalConfig, error) {
	rawLocalConfig, err := loadLocalConfig(path)
	if err != nil {
		return nil, err
	}

	localConfigIndex := make(map[string]*LocalConfigEntry, 0)
	for _, n := range rawLocalConfig.Key {
		localConfigIndex[n.Name] = &LocalConfigEntry{
			Text:  strings.TrimSpace(n.Text),
			Value: n.Value,
		}
	}

	requiredFields := []string{
		LocalConfigLdapMasterURL,
		LocalConfigLdapURL,
		LocalConfigLdapUserDn,
		LocalConfigLdapPassword,
		LocalConfigServerHostname,
	}

	for _, field := range requiredFields {
		_, present := localConfigIndex[field]
		if !present {
			return nil, errors.New("Carbonio is not correctly initialized, please run carbonio-bootstrap (missing required field '" + field + "' in localconfig)")
		}
	}

	return &indexedLocalConfig{localConfigIndex: localConfigIndex}, nil
}

// Value perform a value lookup in the Zimbra local configuration.
func (l *indexedLocalConfig) Value(key string) string {
	return l.localConfigIndex[key].Value
}

// Value perform a value lookup in the Zimbra local configuration
// and extracts one or multiple values, split by a space ' '.
func (l *indexedLocalConfig) Values(key string) []string {
	values := strings.Split(
		strings.Trim(l.localConfigIndex[key].Value, " "),
		" ",
	)
	return values
}

// Text represents an additional description for that key.
func (l *indexedLocalConfig) Text(key string) string {
	return l.localConfigIndex[key].Text
}
