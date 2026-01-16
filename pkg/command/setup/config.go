// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

// ACLConfig represents the ACL configuration for Consul.
type ACLConfig struct {
	Enabled                bool   `json:"enabled"`
	DefaultPolicy          string `json:"default_policy"`
	DownPolicy             string `json:"down_policy"`
	EnableTokenPersistence bool   `json:"enable_token_persistence"`
}

// UIConfig represents the UI configuration for Consul.
type UIConfig struct {
	Enabled bool `json:"enabled"`
}

// PortsConfig represents the ports configuration for Consul.
type PortsConfig struct {
	Grpc    int `json:"grpc"`
	GrpcTLS int `json:"grpc_tls"`
}

// TLSDefaults represents the default TLS settings.
type TLSDefaults struct {
	CaFile         string `json:"ca_file"`
	CertFile       string `json:"cert_file"`
	KeyFile        string `json:"key_file"`
	VerifyIncoming bool   `json:"verify_incoming"`
	VerifyOutgoing bool   `json:"verify_outgoing"`
}

// TLSInternalRPC represents the internal RPC TLS settings.
type TLSInternalRPC struct {
	VerifyServerHostname bool `json:"verify_server_hostname"`
}

// TLSConfig represents the complete TLS configuration.
type TLSConfig struct {
	Defaults    TLSDefaults    `json:"defaults"`
	InternalRPC TLSInternalRPC `json:"internal_rpc"`
}

// DefaultACLConfig returns the standard ACL configuration used across setups.
func DefaultACLConfig() ACLConfig {
	return ACLConfig{
		Enabled:                true,
		DefaultPolicy:          "deny",
		DownPolicy:             "extend-cache",
		EnableTokenPersistence: true,
	}
}

// DefaultUIConfig returns the standard UI configuration.
func DefaultUIConfig() UIConfig {
	return UIConfig{
		Enabled: true,
	}
}

// DefaultPortsConfig returns the standard ports configuration.
func DefaultPortsConfig() PortsConfig {
	return PortsConfig{
		Grpc:    8502,
		GrpcTLS: 8503,
	}
}

// DefaultTLSInternalRPC returns the standard internal RPC TLS settings.
func DefaultTLSInternalRPC() TLSInternalRPC {
	return TLSInternalRPC{
		VerifyServerHostname: true,
	}
}
