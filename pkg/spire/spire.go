// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package spire contains everything service-discover needs to integrate SPIFFE/SPIRE: the identities used in the
// mesh, the configuration generated for the SPIRE components and the join bundle handed to agents that have to join
// the cluster without the cluster credentials tarball.
package spire

const (
	// TrustDomain is the SPIFFE trust domain of the whole mesh.
	TrustDomain = "carbonio"

	// AudienceAutoConfig is the audience of the JWT-SVID a Consul agent uses to join the cluster via auto_config.
	AudienceAutoConfig = "consul-auto-config"
	// AudienceLogin is the audience of the JWT-SVID a service uses to log in the Consul "spire" auth method.
	AudienceLogin = "consul-login"

	// AuthMethodName is the name of the Consul jwt auth method validating the JWT-SVIDs issued by SPIRE.
	AuthMethodName = "spire"

	// ServerPort is the port of the SPIRE server API, exposed on the bind address of the server.
	ServerPort = 8081
	// OIDCPort is the HTTPS port of the SPIRE OIDC discovery provider, exposed on the bind address of the server.
	OIDCPort = 8082
	// OIDCPortString is OIDCPort as it appears in URLs.
	OIDCPortString = "8082"

	ConfigDir      = "/etc/zextras/spire"
	ServerConfFile = "server.conf"
	OIDCConfFile   = "oidc-discovery-provider.conf"
	AgentConfFile  = "agent.conf"
	BundleFile     = "bundle.pem"
	OIDCCertFile   = "oidc-cert.pem"
	OIDCKeyFile    = "oidc-key.pem"
	ServerSocket   = "/run/spire/server/api.sock"
	AgentSocket    = "/run/spire/agent/api.sock"
	ServerDataDir  = "/var/lib/spire/server"
	AgentDataDir   = "/var/lib/spire/agent"
	ServerBin      = "/usr/bin/spire-server"
	AgentBin       = "/usr/bin/spire-agent"
	ServerUnit     = "spire-server.service"
	OIDCUnit       = "spire-oidc-discovery-provider.service"
	AgentUnit      = "spire-agent.service"
	ServerUser     = "spire-server"
	AgentUser      = "spire-agent"
	JWTSVIDTTL     = "5m"
	// ClusterJWKSURLFile is the optional file of the cluster credentials telling where the Consul servers fetch the
	// keys validating the JWT-SVIDs from.
	ClusterJWKSURLFile = "spire-jwks-url"
)

const spiffeScheme = "spiffe://"

// NodeID returns the SPIFFE ID of the SPIRE agent running on the given host.
func NodeID(host string) string {
	return spiffeScheme + TrustDomain + "/node/" + host
}

// ConsulID returns the SPIFFE ID a Consul agent presents to join the cluster. The name is the Consul node name.
func ConsulID(consulNodeName string) string {
	return spiffeScheme + TrustDomain + "/consul/" + consulNodeName
}

// ServiceID returns the SPIFFE ID of a service running in the mesh.
func ServiceID(service string) string {
	return spiffeScheme + TrustDomain + "/service/" + service
}

// ClaimAssertion is the Consul auto_config claim assertion binding the JWT subject to the joining node name. The
// ${node} placeholder is expanded by Consul.
const ClaimAssertion = `value.spiffe_id == "` + spiffeScheme + TrustDomain + `/consul/${node}"`

// Issuer is the issuer of every JWT-SVID, whichever SPIRE server of the cluster signs it. Consul binds a single
// issuer, so it cannot be the URL of one server.
const Issuer = spiffeScheme + TrustDomain

// LocalJWKSURL is where a Consul server fetches the keys validating the JWT-SVIDs when every Consul server runs its
// own SPIRE server and OIDC discovery provider (high availability).
const LocalJWKSURL = "https://127.0.0.1:" + OIDCPortString + "/keys"

// JWKSURL returns the keys endpoint of the OIDC discovery provider of host, used when only one server runs SPIRE.
func JWKSURL(host string) string {
	return "https://" + host + ":" + OIDCPortString + "/keys"
}
