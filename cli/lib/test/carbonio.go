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

package test

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"testing"
	"text/template"
)

const localConfigTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<localconfig>
{{if ne .Hostname ""}}
<key name="zimbra_server_hostname">
  <value>{{.Hostname}}</value>
</key>
{{- end}}
{{if ne .LdapMasterUrl ""}}
<key name="ldap_master_url">
  <value>{{.LdapMasterUrl}}</value>
</key>
{{- end}}
{{if ne .LdapUrl ""}}
<key name="ldap_url">
  <value>{{.LdapUrl}}</value>
</key>
{{- end}}
{{if ne .LdapUserDN ""}}
<key name="zimbra_ldap_userdn">
  <value>{{.LdapUserDN}}</value>
</key>
{{- end}}
{{if ne .LdapPassword ""}}
<key name="zimbra_ldap_password">
  <value>{{.LdapPassword}}</value>
</key>
{{- end}}
</localconfig>`
const DefaultLdapUserDN = "uid=zimbra,cn=admins,cn=zimbra"

type localConfigFields struct {
	Hostname      string
	LdapMasterUrl string
	LdapUrl       string
	LdapUserDN    string
	LdapPassword  string
}

func GenerateLocalConfig(
	t *testing.T,
	hostname string,
	ldapMaserUrl string,
	ldapUrl string,
	ldapUserDN string,
	ldapPassword string,
) []byte {
	res := bytes.Buffer{}
	ldapData := &localConfigFields{
		Hostname:      hostname,
		LdapMasterUrl: ldapMaserUrl,
		LdapUrl:       ldapUrl,
		LdapUserDN:    ldapUserDN,
		LdapPassword:  ldapPassword,
	}
	localConfigTemplate := template.Must(template.New("localconfig").Parse(localConfigTemplate))
	assert.NoError(t, localConfigTemplate.Execute(&res, ldapData))
	data, err := ioutil.ReadAll(&res)
	assert.NoError(t, err)
	return data
}
