// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package test

import (
	"bytes"
	"io"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
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
	data, err := io.ReadAll(&res)
	assert.NoError(t, err)
	return data
}
