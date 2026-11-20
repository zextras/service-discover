// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
)

// ServerConf renders the configuration of the SPIRE server listening on bindAddress. The JWT issuer is the same for
// every server of the cluster. An empty datastoreDSN selects a local SQLite database (a single SPIRE server), a
// PostgreSQL connection string selects the database shared by all the SPIRE servers of a high availability setup.
func ServerConf(bindAddress, datastoreDSN string) string {
	databaseType, connectionString := "sqlite3", filepath.Join(ServerDataDir, "datastore.sqlite3")
	if datastoreDSN != "" {
		databaseType, connectionString = "postgres", datastoreDSN
	}

	return fmt.Sprintf(`server {
  bind_address = %q
  bind_port = "%d"
  socket_path = %q
  trust_domain = %q
  data_dir = %q
  log_level = "INFO"
  jwt_issuer = %q
  default_jwt_svid_ttl = %q
}

plugins {
  DataStore "sql" {
    plugin_data {
      database_type = %q
      connection_string = %q
    }
  }
  NodeAttestor "join_token" {
    plugin_data {}
  }
  KeyManager "disk" {
    plugin_data {
      keys_path = %q
    }
  }
}
`,
		bindAddress, ServerPort, ServerSocket, TrustDomain, ServerDataDir, Issuer, JWTSVIDTTL,
		databaseType, connectionString,
		filepath.Join(ServerDataDir, "keys.json"),
	)
}

// OIDCConf renders the configuration of the SPIRE OIDC discovery provider. The HTTPS endpoint listens on
// bindAddress and is served with the certificate and key found in configDir. It also answers to 127.0.0.1: every
// Consul server validates the JWT-SVIDs against the provider running on the same host.
func OIDCConf(configDir, bindAddress, hostname string) string {
	return fmt.Sprintf(`domains = [%q, %q, "127.0.0.1"]
log_level = "INFO"

serving_cert_source "cert_file" {
  cert_file_path = %q
  key_file_path = %q
  addr = %q
}

server_api {
  address = "unix://%s"
}
`,
		hostname, bindAddress,
		filepath.Join(configDir, OIDCCertFile),
		filepath.Join(configDir, OIDCKeyFile),
		net.JoinHostPort(bindAddress, strconv.Itoa(OIDCPort)),
		ServerSocket,
	)
}

// AgentConf renders the configuration of a SPIRE agent attesting itself to serverHost:serverPort with a one-time join
// token.
func AgentConf(configDir, serverHost string, serverPort int, joinToken string) string {
	return fmt.Sprintf(`agent {
  data_dir = %q
  log_level = "INFO"
  server_address = %q
  server_port = "%d"
  socket_path = %q
  trust_domain = %q
  trust_bundle_path = %q
  join_token = %q
}

plugins {
  NodeAttestor "join_token" {
    plugin_data {}
  }
  KeyManager "disk" {
    plugin_data {
      directory = %q
    }
  }
  WorkloadAttestor "unix" {
    plugin_data {}
  }
}
`,
		AgentDataDir, serverHost, serverPort, AgentSocket, TrustDomain,
		filepath.Join(configDir, BundleFile), joinToken, AgentDataDir,
	)
}
