// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/exec"
)

const authMethodNotFound = "not found"

// authMethodConfig is the configuration of the Consul jwt auth method validating the JWT-SVIDs issued by SPIRE.
type authMethodConfig struct {
	OIDCDiscoveryURL    string            `json:"OIDCDiscoveryURL"`
	OIDCDiscoveryCACert string            `json:"OIDCDiscoveryCACert"`
	BoundIssuer         string            `json:"BoundIssuer"`
	BoundAudiences      []string          `json:"BoundAudiences"`
	ClaimMappings       map[string]string `json:"ClaimMappings"`
}

// EnsureAuthMethod creates the "spire" jwt auth method in Consul, issuing local tokens for the JWT-SVIDs with the
// consul-login audience, or updates it when it already exists. rootToken needs acl:write.
func EnsureAuthMethod(
	commandCreator func(name string, args ...string) exec.Cmd, rootToken, oidcURL, caPEM string,
) error {
	err := os.Setenv(command.ConsulHTTPToken, rootToken)
	if err != nil {
		return errors.WithMessage(err, "unable to set the Consul token before configuring the auth method")
	}
	defer os.Unsetenv(command.ConsulHTTPToken)

	cfg, err := json.Marshal(authMethodConfig{
		OIDCDiscoveryURL:    oidcURL,
		OIDCDiscoveryCACert: caPEM,
		BoundIssuer:         oidcURL,
		BoundAudiences:      []string{AudienceLogin},
		ClaimMappings:       map[string]string{"sub": "spiffe_id"},
	})
	if err != nil {
		return err
	}

	_, err = commandCreator(command.ConsulBin, "acl", "auth-method", "read", "-name", AuthMethodName).Output()
	if err == nil {
		_, err = commandCreator(command.ConsulBin, "acl", "auth-method", "update",
			"-name", AuthMethodName, "-config", string(cfg)).Output()
		if err != nil {
			return exec.ErrorFromStderr(err, "unable to update the Consul auth method "+AuthMethodName)
		}

		return nil
	}

	if !strings.Contains(exec.ErrorFromStderr(err, "").Error(), authMethodNotFound) {
		return exec.ErrorFromStderr(err, "unable to read the Consul auth method "+AuthMethodName)
	}

	_, err = commandCreator(command.ConsulBin, "acl", "auth-method", "create",
		"-type", "jwt", "-name", AuthMethodName, "-token-locality", "local", "-config", string(cfg)).Output()
	if err != nil {
		return exec.ErrorFromStderr(err, "unable to create the Consul auth method "+AuthMethodName)
	}

	return nil
}
