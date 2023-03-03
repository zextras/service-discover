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

package carbonio

import (
	"github.com/Zextras/service-discover/cli/lib/test"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestLoadLocalConfig(t *testing.T) {
	t.Parallel()
	type args struct {
		path string
	}
	tests := []struct {
		name    string
		args    args
		want    LocalConfig
		wantErr bool
	}{
		{
			name: "Return a valid LocalConfig for a valid path",
			args: args{
				path: generateAndPopulateCorrectLocalConfig("Return a valid LocalConfig for a valid path").Name(),
			},
			want: &indexedLocalConfig{map[string]*LocalConfigEntry{
				"ssl_default_digest": {
					Text:  "",
					Value: "sha256",
				},
				"mailboxd_java_heap_size": {
					Text:  "",
					Value: "1024",
				},
				"ldap_nginx_password": {
					Text:  "",
					Value: "password",
				},
				"ldap_master_url": {
					Text:  "",
					Value: "ldap://mail.example.com:389 ldap://mail2.example.com:389",
				},
				"ssl_allow_mismatched_certs": {
					Text:  "",
					Value: "true",
				},
				"zimbra_java_home": {
					Text:  "",
					Value: "/opt/zextras/common/lib/jvm/java",
				},
				"ldap_port": {
					Text:  "",
					Value: "389",
				},
				"mailboxd_keystore": {
					Text:  "",
					Value: "/opt/zextras/mailboxd/etc/keystore",
				},
				"zimbra_ldap_password": {
					Text:  "",
					Value: "password",
				},
				"mailboxd_keystore_password": {
					Text:  "",
					Value: "password",
				},
				"mailboxd_truststore": {
					Text:  "",
					Value: "/opt/zextras/common/lib/jvm/java/lib/security/cacerts",
				},
				"av_notify_user": {
					Text:  "",
					Value: "admin@mail.example.com",
				},
				"mailboxd_directory": {
					Text:  "",
					Value: "/opt/zextras/mailboxd",
				},
				"av_notify_domain": {
					Text:  "",
					Value: "mail.example.com",
				},
				"zimbra_zmjava_options": {
					Text:  "",
					Value: "-Xmx256m -Dhttps.protocols=TLSv1,TLSv1.1,TLSv1.2 -Djdk.tls.client.protocols=TLSv1,TLSv1.1,TLSv1.2 -Djava.net.preferIPv4Stack=true",
				},
				"zimbra_require_interprocess_security": {
					Text:  "",
					Value: "0",
				},
				"smtp_destination": {
					Text:  "",
					Value: "admin@mail.example.com",
				},
				"zimbra_mail_service_port": {
					Text:  "",
					Value: "8080",
				},
				"zimbra_gid": {
					Text:  "",
					Value: "1000",
				},
				"ldap_amavis_password": {
					Text:  "",
					Value: "password",
				},
				"mysql_bind_address": {
					Text:  "",
					Value: "127.0.0.1",
				},
				"mailboxd_truststore_password": {
					Text:  "",
					Value: "password",
				},
				"ldap_host": {
					Text:  "",
					Value: "mail.example.com",
				},
				"zmtrainsa_cleanup_host": {
					Text:  "",
					Value: "true",
				},
				"ldap_url": {
					Text:  "",
					Value: "ldap://mail.example.com:389 ldap://mail2.example.com:389",
				},
				"antispam_mysql_host": {
					Text:  "",
					Value: "127.0.0.1",
				},
				"ldap_starttls_supported": {
					Text:  "",
					Value: "0",
				},
				"zimbra_zmprov_default_to_ldap": {
					Text:  "",
					Value: "false",
				},
				"smtp_source": {
					Text:  "",
					Value: "admin@mail.example.com",
				},
				"zimbra_uid": {
					Text:  "",
					Value: "1000",
				},
				"ssl_allow_untrusted_certs": {
					Text:  "",
					Value: "false",
				},
				"zimbra_user": {
					Text:  "",
					Value: "zimbra",
				},
				"mailboxd_java_options": {
					Text:  "",
					Value: "-server -Dhttps.protocols=TLSv1,TLSv1.1,TLSv1.2 -Djdk.tls.client.protocols=TLSv1,TLSv1.1,TLSv1.2 -Djava.awt.headless=true -Dsun.net.inetaddr.ttl=${networkaddress_cache_ttl} -Dorg.apache.jasper.compiler.disablejsr199=true -XX:+UseG1GC -XX:SoftRefLRUPolicyMSPerMB=1 -XX:+UnlockExperimentalVMOptions -XX:G1NewSizePercent=15 -XX:G1MaxNewSizePercent=45 -XX:-OmitStackTraceInFastThrow -verbose:gc -Xlog:gc*=info,safepoint=info:file=/opt/zextras/log/gc.log:time:filecount=20,filesize=10m -Djava.net.preferIPv4Stack=true -agentlib:jdwp=transport=dt_socket,server=y,suspend=n,address=*:5005 -Dcom.sun.management.jmxremote -Dcom.sun.management.jmxremote.port=5000 -Dcom.sun.management.jmxremote.authenticate=false -Dcom.sun.management.jmxremote.ssl=false",
				},
				"ldap_is_master": {
					Text:  "",
					Value: "true",
				},
				"ldap_replication_password": {
					Text:  "",
					Value: "password",
				},
				"postfix_setgid_group": {
					Text:  "",
					Value: "postdrop",
				},
				"zimbra_mysql_password": {
					Text:  "",
					Value: "password",
				},
				"ldap_postfix_password": {
					Text:  "",
					Value: "password",
				},
				"zimbra_server_hostname": {
					Text:  "",
					Value: "mail.example.com",
				},
				"mysql_root_password": {
					Text:  "",
					Value: "password",
				},
				"ldap_root_password": {
					Text:  "",
					Value: "password",
				},
				"mailboxd_server": {
					Text:  "",
					Value: "jetty",
				},
				"postfix_mail_owner": {
					Text:  "",
					Value: "postfix",
				},
				"zimbra_admin_service_port": {
					Text:  "",
					Value: "9071",
				},
				"zimbra_ldap_userdn": {
					Text:  "",
					Value: "uid=zimbra,cn=admins,cn=zimbra",
				},
				"ldap_bes_searcher_password": {
					Text:  "",
					Value: "password",
				},
				"proxy_server_names_hash_bucket_size": {
					Text:  "",
					Value: "256",
				},
				"zimbra_mysql_connector_maxActive": {
					Text:  "",
					Value: "100",
				},
			}},
			wantErr: false,
		},
		{
			name: "Returns an error on a config that's not correct",
			args: args{
				path: generateWrongLocalConfig("Returns an error on a config that's not correct").Name(),
			},
			want:    nil,
			wantErr: true,
		},
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer os.Remove(tt.args.path)

			localConfig, err := LoadLocalConfig(tt.args.path)
			if tt.wantErr {
				assert.Error(t, err, "The localconfig parsing should have failed in some way")
				assert.Nil(t, localConfig, "The localconfig instance should not be exist at all")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, localConfig, "The two objects should contain the same elements")
			}
		})
	}
}

func generateWrongLocalConfig(testName string) *os.File {
	tmpFile := test.GenerateRandomFile(testName)
	if err := os.WriteFile(tmpFile.Name(), []byte("surely this is not an xml"), os.FileMode(0755)); err != nil {
		panic(err)
	}

	return tmpFile
}

func generateAndPopulateCorrectLocalConfig(testName string) *os.File {
	tmpFile := test.GenerateRandomFile(testName)

	// Actual localconfig.xml file taken and redacted from a Zimbra installation
	if err := os.WriteFile(tmpFile.Name(), []byte(`<?xml version="1.0" encoding="UTF-8"?>

<localconfig>
  <key name="ssl_default_digest">
    <value>sha256</value>
  </key>
  <key name="mailboxd_java_heap_size">
    <value>1024</value>
  </key>
  <key name="ldap_nginx_password">
    <value>password</value>
  </key>
  <key name="ldap_master_url">
    <value>ldap://mail.example.com:389 ldap://mail2.example.com:389</value>
  </key>
  <key name="ssl_allow_mismatched_certs">
    <value>true</value>
  </key>
  <key name="zimbra_java_home">
    <value>/opt/zextras/common/lib/jvm/java</value>
  </key>
  <key name="ldap_port">
    <value>389</value>
  </key>
  <key name="mailboxd_keystore">
    <value>/opt/zextras/mailboxd/etc/keystore</value>
  </key>
  <key name="zimbra_ldap_password">
    <value>password</value>
  </key>
  <key name="mailboxd_keystore_password">
    <value>password</value>
  </key>
  <key name="mailboxd_truststore">
    <value>/opt/zextras/common/lib/jvm/java/lib/security/cacerts</value>
  </key>
  <key name="av_notify_user">
    <value>admin@mail.example.com</value>
  </key>
  <key name="mailboxd_directory">
    <value>/opt/zextras/mailboxd</value>
  </key>
  <key name="av_notify_domain">
    <value>mail.example.com</value>
  </key>
  <key name="zimbra_zmjava_options">
    <value>-Xmx256m -Dhttps.protocols=TLSv1,TLSv1.1,TLSv1.2 -Djdk.tls.client.protocols=TLSv1,TLSv1.1,TLSv1.2 -Djava.net.preferIPv4Stack=true</value>
  </key>
  <key name="zimbra_require_interprocess_security">
    <value>0</value>
  </key>
  <key name="smtp_destination">
    <value>admin@mail.example.com</value>
  </key>
  <key name="zimbra_mail_service_port">
    <value>8080</value>
  </key>
  <key name="zimbra_gid">
    <value>1000</value>
  </key>
  <key name="ldap_amavis_password">
    <value>password</value>
  </key>
  <key name="mysql_bind_address">
    <value>127.0.0.1</value>
  </key>
  <key name="mailboxd_truststore_password">
    <value>password</value>
  </key>
  <key name="ldap_host">
    <value>mail.example.com</value>
  </key>
  <key name="zmtrainsa_cleanup_host">
    <value>true</value>
  </key>
  <key name="ldap_url">
	<value>ldap://mail.example.com:389 ldap://mail2.example.com:389</value>
  </key>
  <key name="antispam_mysql_host">
    <value>127.0.0.1</value>
  </key>
  <key name="ldap_starttls_supported">
    <value>0</value>
  </key>
  <key name="zimbra_zmprov_default_to_ldap">
    <value>false</value>
  </key>
  <key name="smtp_source">
    <value>admin@mail.example.com</value>
  </key>
  <key name="zimbra_uid">
    <value>1000</value>
  </key>
  <key name="ssl_allow_untrusted_certs">
    <value>false</value>
  </key>
  <key name="zimbra_user">
    <value>zimbra</value>
  </key>
  <key name="mailboxd_java_options">
    <value>-server -Dhttps.protocols=TLSv1,TLSv1.1,TLSv1.2 -Djdk.tls.client.protocols=TLSv1,TLSv1.1,TLSv1.2 -Djava.awt.headless=true -Dsun.net.inetaddr.ttl=${networkaddress_cache_ttl} -Dorg.apache.jasper.compiler.disablejsr199=true -XX:+UseG1GC -XX:SoftRefLRUPolicyMSPerMB=1 -XX:+UnlockExperimentalVMOptions -XX:G1NewSizePercent=15 -XX:G1MaxNewSizePercent=45 -XX:-OmitStackTraceInFastThrow -verbose:gc -Xlog:gc*=info,safepoint=info:file=/opt/zextras/log/gc.log:time:filecount=20,filesize=10m -Djava.net.preferIPv4Stack=true -agentlib:jdwp=transport=dt_socket,server=y,suspend=n,address=*:5005 -Dcom.sun.management.jmxremote -Dcom.sun.management.jmxremote.port=5000 -Dcom.sun.management.jmxremote.authenticate=false -Dcom.sun.management.jmxremote.ssl=false</value>
  </key>
  <key name="ldap_is_master">
    <value>true</value>
  </key>
  <key name="ldap_replication_password">
    <value>password</value>
  </key>
  <key name="postfix_setgid_group">
    <value>postdrop</value>
  </key>
  <key name="zimbra_mysql_password">
    <value>password</value>
  </key>
  <key name="ldap_postfix_password">
    <value>password</value>
  </key>
  <key name="zimbra_server_hostname">
    <value>mail.example.com</value>
  </key>
  <key name="mysql_root_password">
    <value>password</value>
  </key>
  <key name="ldap_root_password">
    <value>password</value>
  </key>
  <key name="mailboxd_server">
    <value>jetty</value>
  </key>
  <key name="postfix_mail_owner">
    <value>postfix</value>
  </key>
  <key name="zimbra_admin_service_port">
    <value>9071</value>
  </key>
  <key name="zimbra_ldap_userdn">
    <value>uid=zimbra,cn=admins,cn=zimbra</value>
  </key>
  <key name="ldap_bes_searcher_password">
    <value>password</value>
  </key>
  <key name="proxy_server_names_hash_bucket_size">
    <value>256</value>
  </key>
  <key name="zimbra_mysql_connector_maxActive">
    <value>100</value>
  </key>
</localconfig>`), os.FileMode(0755)); err != nil {
		panic(err)
	}
	return tmpFile
}
