// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/consul/api/v2"
)

// consulCluster implements Cluster on the Consul HTTP API.
type consulCluster struct {
	acl     *api.ACL
	entries *api.ConfigEntries
}

// NewCluster returns a Cluster talking to the Consul agent configured by the CONSUL_HTTP_* environment variables,
// with the given token. The token needs to be able to write ACLs and config entries: the bootstrap token.
func NewCluster(token string) (Cluster, error) {
	cfg := api.DefaultConfig()
	cfg.Token = token

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	// Fail on a missing or wrong token here, so that a later "not found" always refers to the object looked up.
	_, _, err = client.ACL().TokenReadSelf(nil)
	if err != nil {
		if isDenied(err) {
			return nil, fmt.Errorf("the Consul token is not valid: %w", err)
		}

		return nil, fmt.Errorf("unable to reach Consul: %w", err)
	}

	return &consulCluster{acl: client.ACL(), entries: client.ConfigEntries()}, nil
}

func (c *consulCluster) PolicyByName(name string) (*api.ACLPolicy, error) {
	policy, _, err := c.acl.PolicyReadByName(name, nil)

	return policy, err
}

func (c *consulCluster) PolicyCreate(policy *api.ACLPolicy) error {
	_, _, err := c.acl.PolicyCreate(policy, nil)

	return err
}

func (c *consulCluster) PolicyUpdate(policy *api.ACLPolicy) error {
	_, _, err := c.acl.PolicyUpdate(policy, nil)

	return err
}

func (c *consulCluster) RoleByName(name string) (*api.ACLRole, error) {
	role, _, err := c.acl.RoleReadByName(name, nil)

	return role, err
}

func (c *consulCluster) RoleCreate(role *api.ACLRole) error {
	_, _, err := c.acl.RoleCreate(role, nil)

	return err
}

func (c *consulCluster) RoleUpdate(role *api.ACLRole) error {
	_, _, err := c.acl.RoleUpdate(role, nil)

	return err
}

func (c *consulCluster) BindingRules(authMethod string) ([]*api.ACLBindingRule, error) {
	rules, _, err := c.acl.BindingRuleList(authMethod, nil)

	return rules, err
}

func (c *consulCluster) BindingRuleCreate(rule *api.ACLBindingRule) error {
	_, _, err := c.acl.BindingRuleCreate(rule, nil)

	return err
}

func (c *consulCluster) BindingRuleUpdate(rule *api.ACLBindingRule) error {
	_, _, err := c.acl.BindingRuleUpdate(rule, nil)

	return err
}

// ConfigEntry returns the config entry, nil when it does not exist.
func (c *consulCluster) ConfigEntry(kind, name string) (api.ConfigEntry, error) {
	entry, _, err := c.entries.Get(kind, name, nil)
	if err != nil {
		statusErr, ok := errors.AsType[api.StatusError](err)
		if ok && statusErr.Code == http.StatusNotFound {
			return nil, nil //nolint:nilnil // nil is the documented answer for an entry that does not exist.
		}

		return nil, err
	}

	return entry, nil
}

func (c *consulCluster) SetConfigEntry(entry api.ConfigEntry) error {
	_, _, err := c.entries.Set(entry, nil)

	return err
}

// consulLogin implements the token operations of the sync on the local Consul agent.
type consulLogin struct {
	client     *api.Client
	authMethod string
}

func newConsulLogin(authMethod string) (*consulLogin, error) {
	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return nil, err
	}

	return &consulLogin{client: client, authMethod: authMethod}, nil
}

// Login exchanges a JWT-SVID with a token of the service role.
func (c *consulLogin) Login(jwt string) (string, error) {
	token, _, err := c.client.ACL().Login(&api.ACLLoginParams{AuthMethod: c.authMethod, BearerToken: jwt}, nil)
	if err != nil {
		return "", err
	}

	return token.SecretID, nil
}

// Valid reports whether the token is still accepted by the cluster. An unreachable agent is an error: it must not be
// mistaken for an expired token.
func (c *consulLogin) Valid(secret string) (bool, error) {
	_, _, err := c.client.ACL().TokenReadSelf(&api.QueryOptions{Token: secret})
	if err == nil {
		return true, nil
	}

	if isDenied(err) {
		return false, nil
	}

	return false, err
}

// IsLogin reports whether the token was created by a login of the auth method: only these can be logged out, the
// tokens created by hand have no auth method.
func (c *consulLogin) IsLogin(secret string) (bool, error) {
	token, _, err := c.client.ACL().TokenReadSelf(&api.QueryOptions{Token: secret})
	if err != nil {
		return false, err
	}

	return token.AuthMethod != "", nil
}

// isDenied reports whether Consul refused the token: it is unknown, expired or not allowed.
func isDenied(err error) bool {
	statusErr, ok := errors.AsType[api.StatusError](err)

	return ok && (statusErr.Code == http.StatusForbidden || statusErr.Code == http.StatusUnauthorized)
}

// Logout destroys a token created by Login.
func (c *consulLogin) Logout(secret string) error {
	_, err := c.client.ACL().Logout(&api.WriteOptions{Token: secret})

	return err
}

// consulService implements Consul on the local agent with the token of a service.
type consulService struct {
	client *api.Client
}

func newConsulService(token string) (Consul, error) {
	cfg := api.DefaultConfig()
	cfg.Token = token

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &consulService{client: client}, nil
}

func (c *consulService) Get(key string) (string, bool, error) {
	pair, _, err := c.client.KV().Get(key, nil)
	if err != nil {
		return "", false, err
	}

	if pair == nil {
		return "", false, nil
	}

	return string(pair.Value), true, nil
}

func (c *consulService) Put(key, value string) error {
	_, err := c.client.KV().Put(&api.KVPair{Key: key, Value: []byte(value)}, nil)

	return err
}

func (c *consulService) Delete(key string) error {
	_, err := c.client.KV().Delete(key, nil)

	return err
}

func (c *consulService) Instances(service string, passing bool) ([]Instance, error) {
	entries, _, err := c.client.Health().Service(service, "", passing, nil)
	if err != nil {
		return nil, err
	}

	instances := make([]Instance, 0, len(entries))

	for _, entry := range entries {
		address := entry.Service.Address
		if address == "" {
			address = entry.Node.Address
		}

		instances = append(instances, Instance{Address: address, Meta: entry.Service.Meta})
	}

	return instances, nil
}

func (c *consulService) SetConfigEntry(entry api.ConfigEntry) error {
	_, _, err := c.client.ConfigEntries().Set(entry, nil)

	return err
}
