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

const (
	authMethodNotFound = "not found"
	authMethodCmd      = "auth-method"
)

// authMethodConfig is the configuration of the Consul jwt auth method validating the JWT-SVIDs issued by SPIRE.
type authMethodConfig struct {
	JWKSURL        string            `json:"JWKSURL"`
	JWKSCACert     string            `json:"JWKSCACert"`
	BoundIssuer    string            `json:"BoundIssuer"`
	BoundAudiences []string          `json:"BoundAudiences"`
	ClaimMappings  map[string]string `json:"ClaimMappings"`
}

// EnsureAuthMethod creates the "spire" jwt auth method in Consul, issuing local tokens for the JWT-SVIDs with the
// consul-login audience, or updates it when it already exists. rootToken needs acl:write. Consul fetches the keys at
// jwksURL while saving the method, from the server handling the request (the Raft leader): the endpoint must be
// reachable from there.
func EnsureAuthMethod(
	commandCreator func(name string, args ...string) exec.Cmd, rootToken, jwksURL, caPEM string,
) error {
	err := os.Setenv(command.ConsulHTTPToken, rootToken)
	if err != nil {
		return errors.WithMessage(err, "unable to set the Consul token before configuring the auth method")
	}
	defer os.Unsetenv(command.ConsulHTTPToken)

	cfg, err := json.Marshal(authMethodConfig{
		JWKSURL:        jwksURL,
		JWKSCACert:     caPEM,
		BoundIssuer:    Issuer,
		BoundAudiences: []string{AudienceLogin},
		ClaimMappings:  map[string]string{"sub": "spiffe_id"},
	})
	if err != nil {
		return err
	}

	_, err = commandCreator(command.ConsulBin, "acl", authMethodCmd, "read", "-name", AuthMethodName).Output()
	if err == nil {
		_, err = commandCreator(command.ConsulBin, "acl", authMethodCmd, "update",
			"-name", AuthMethodName, "-config", string(cfg)).Output()
		if err != nil {
			return exec.ErrorFromStderr(err, "unable to update the Consul auth method "+AuthMethodName)
		}

		return nil
	}

	if !strings.Contains(exec.ErrorFromStderr(err, "").Error(), authMethodNotFound) {
		return exec.ErrorFromStderr(err, "unable to read the Consul auth method "+AuthMethodName)
	}

	_, err = commandCreator(command.ConsulBin, "acl", authMethodCmd, "create",
		"-type", "jwt", "-name", AuthMethodName, "-token-locality", "local", "-config", string(cfg)).Output()
	if err != nil {
		return exec.ErrorFromStderr(err, "unable to create the Consul auth method "+AuthMethodName)
	}

	return nil
}

// AuthMethodJWKSURL returns the keys URL configured in the "spire" auth method, and whether the method exists.
func AuthMethodJWKSURL(
	commandCreator func(name string, args ...string) exec.Cmd, rootToken string,
) (string, bool, error) {
	err := os.Setenv(command.ConsulHTTPToken, rootToken)
	if err != nil {
		return "", false, errors.WithMessage(err, "unable to set the Consul token before reading the auth method")
	}
	defer os.Unsetenv(command.ConsulHTTPToken)

	out, err := commandCreator(command.ConsulBin, "acl", authMethodCmd, "read",
		"-name", AuthMethodName, "-format", "json").Output()
	if err != nil {
		if strings.Contains(exec.ErrorFromStderr(err, "").Error(), authMethodNotFound) {
			return "", false, nil
		}

		return "", false, exec.ErrorFromStderr(err, "unable to read the Consul auth method "+AuthMethodName)
	}

	method := struct {
		Config authMethodConfig `json:"Config"`
	}{}

	err = json.Unmarshal(out, &method)
	if err != nil {
		return "", false, errors.WithMessage(err, "unable to decode the Consul auth method "+AuthMethodName)
	}

	return method.Config.JWKSURL, true, nil
}
