// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/consul/api/v2"
)

// serviceTokenNamespace is the UUIDv5 namespace used to derive service token
// AccessorIDs. It MUST stay identical in every implementation of the scheme
// (e.g. carbonio-quarkus-extensions ConsulSetupRunner): changing it orphans
// every token already created.
var serviceTokenNamespace = uuid.MustParse("e6dffb05-3483-451c-af44-cf73dbc22c18")

// legacyTokenDescriptionPrefix is the prefix used by the bash setup scripts
// that created service tokens before the deterministic scheme.
const legacyTokenDescriptionPrefix = "Token for "

var (
	errServiceTokenNotFound = errors.New("service token not found")
	errRedactedSecretID     = errors.New("SecretID is redacted: CONSUL_HTTP_TOKEN needs acl:write")
)

// ServiceTokenAccessorID returns the AccessorID of the ACL token owned by
// service on the Consul node nodeName. The same pair always maps to the same
// ID, so a token can be found again even when its local token file is gone.
func ServiceTokenAccessorID(service, nodeName string) string {
	return uuid.NewSHA1(serviceTokenNamespace, []byte(service+"/"+nodeName)).String()
}

// ServiceTokenDescription returns the description set on newly created
// service tokens.
func ServiceTokenDescription(service, nodeName string) string {
	return "carbonio/" + service + "/" + nodeName
}

// ServiceToken groups the commands managing the ACL token of a service
// running in the service mesh. Both commands need a token with acl:write in
// CONSUL_HTTP_TOKEN (the bootstrap token during pending-setups).
type ServiceToken struct {
	Ensure      EnsureServiceToken       `cmd:"" help:"Create or reuse the ACL token of a service on this node and write it to the token file"`                        //nolint:lll
	PruneLegacy PruneLegacyServiceTokens `cmd:"" name:"prune-legacy" help:"Delete the tokens of a service on this node created before the deterministic token scheme"` //nolint:lll
}

type EnsureServiceToken struct {
	Service   string `required:"" help:"Service name, e.g. carbonio-files"`
	Policy    string `required:"" help:"Name of the ACL policy linked to the token"`
	TokenFile string `required:"" name:"token-file" help:"Path where the token SecretID is written (mode 0600)"`
	Owner     string `optional:"" help:"User (and its primary group) owning the token file"`
}

type PruneLegacyServiceTokens struct {
	Service       string   `required:"" help:"Service name, e.g. carbonio-files"`
	Policy        string   `required:"" help:"Name of the ACL policy linked to the tokens"`
	LegacyService []string `optional:"" name:"legacy-service" help:"Other service name used in legacy token descriptions (repeatable)"` //nolint:lll
}

func (e *EnsureServiceToken) Run(_ *GlobalCommonFlags) error {
	acl, err := newConsulServiceTokenACL()
	if err != nil {
		return err
	}

	secret, err := ensureServiceToken(acl, e.Service, e.Policy)
	if err != nil {
		return err
	}

	return writeTokenFile(e.TokenFile, secret, e.Owner)
}

func (p *PruneLegacyServiceTokens) Run(_ *GlobalCommonFlags) error {
	acl, err := newConsulServiceTokenACL()
	if err != nil {
		return err
	}

	services := append([]string{p.Service}, p.LegacyService...)

	deleted, err := pruneLegacyServiceTokens(acl, p.Service, services, p.Policy, localHostnames())
	for _, accessor := range deleted {
		fmt.Printf("Deleted legacy token %s of %s\n", accessor, p.Service)
	}

	return err
}

// serviceTokenACL is the subset of the Consul API used to manage service
// tokens. TokenRead returns errServiceTokenNotFound when the token does not
// exist.
type serviceTokenACL interface {
	NodeName() (string, error)
	TokenRead(accessorID string) (*api.ACLToken, error)
	TokenCreate(token *api.ACLToken) (*api.ACLToken, error)
	TokenUpdate(token *api.ACLToken) (*api.ACLToken, error)
	TokenListByPolicy(policyName string) ([]*api.ACLTokenListEntry, error)
	TokenDelete(accessorID string) error
}

func ensureServiceToken(acl serviceTokenACL, service, policy string) (string, error) {
	nodeName, err := acl.NodeName()
	if err != nil {
		return "", fmt.Errorf("unable to read the Consul node name: %w", err)
	}

	accessorID := ServiceTokenAccessorID(service, nodeName)

	token, err := acl.TokenRead(accessorID)
	if errors.Is(err, errServiceTokenNotFound) {
		created, err := acl.TokenCreate(&api.ACLToken{
			AccessorID:  accessorID,
			Description: ServiceTokenDescription(service, nodeName),
			Policies:    []*api.ACLTokenPolicyLink{{Name: policy}},
		})
		if err != nil {
			return "", fmt.Errorf("unable to create token %s of %s: %w", accessorID, service, err)
		}

		return created.SecretID, nil
	}

	if err != nil {
		return "", fmt.Errorf("unable to read token %s of %s: %w", accessorID, service, err)
	}

	if !isSecretID(token.SecretID) {
		return "", fmt.Errorf("token %s of %s: %w", accessorID, service, errRedactedSecretID)
	}

	linked := slices.ContainsFunc(token.Policies, func(l *api.ACLTokenPolicyLink) bool { return l.Name == policy })
	if !linked {
		token.Policies = append(token.Policies, &api.ACLTokenPolicyLink{Name: policy})

		_, err = acl.TokenUpdate(token)
		if err != nil {
			return "", fmt.Errorf("unable to link policy %s to token %s: %w", policy, accessorID, err)
		}
	}

	return token.SecretID, nil
}

// pruneLegacyServiceTokens deletes the tokens linked to policy that were
// created for service on this node by the pre-deterministic setup scripts
// (description "Token for <name>/<hostname -A>" or "<name>/<fqdn>", where name
// is one of legacyNames). A token is deleted only when its description names
// exactly one of legacyNames and at least one of hostnames, so tokens of other
// nodes are never touched.
func pruneLegacyServiceTokens(
	acl serviceTokenACL,
	service string,
	legacyNames []string,
	policy string,
	hostnames []string,
) ([]string, error) {
	nodeName, err := acl.NodeName()
	if err != nil {
		return nil, fmt.Errorf("unable to read the Consul node name: %w", err)
	}

	current := ServiceTokenAccessorID(service, nodeName)

	tokens, err := acl.TokenListByPolicy(policy)
	if err != nil {
		return nil, fmt.Errorf("unable to list tokens of policy %s: %w", policy, err)
	}

	var deleted []string

	for _, entry := range tokens {
		if entry.AccessorID == current || !isLegacyServiceToken(entry.Description, legacyNames, hostnames) {
			continue
		}

		err = acl.TokenDelete(entry.AccessorID)
		if err != nil {
			return deleted, fmt.Errorf("unable to delete legacy token %s: %w", entry.AccessorID, err)
		}

		deleted = append(deleted, entry.AccessorID)
	}

	return deleted, nil
}

func isLegacyServiceToken(description string, legacyNames, hostnames []string) bool {
	normalized := strings.Join(strings.Fields(description), " ")
	normalized = strings.TrimPrefix(normalized, legacyTokenDescriptionPrefix)

	service, names, found := strings.Cut(normalized, "/")
	if !found || !slices.Contains(legacyNames, service) {
		return false
	}

	for name := range strings.FieldsSeq(names) {
		if slices.Contains(hostnames, strings.TrimSuffix(name, ".")) {
			return true
		}
	}

	return false
}

func isSecretID(s string) bool {
	_, err := uuid.Parse(s)

	return err == nil
}

// localHostnames returns the names this host is known by: the kernel hostname
// plus the reverse lookups of its addresses, mirroring `hostname -A` used by
// the legacy setup scripts.
func localHostnames() []string {
	hostname, err := os.Hostname()
	if err != nil {
		return nil
	}

	ctx := context.Background()
	resolver := net.DefaultResolver
	names := []string{hostname}

	cname, err := resolver.LookupCNAME(ctx, hostname)
	if err == nil {
		names = append(names, strings.TrimSuffix(cname, "."))
	}

	addrs, _ := resolver.LookupHost(ctx, hostname)
	for _, addr := range addrs {
		ptrs, _ := resolver.LookupAddr(ctx, addr)
		for _, ptr := range ptrs {
			names = append(names, strings.TrimSuffix(ptr, "."))
		}
	}

	slices.Sort(names)

	return slices.Compact(names)
}

// writeTokenFile atomically replaces path with secret, mode 0600, owned by
// owner when set.
func writeTokenFile(path, secret, owner string) error {
	uid, gid := -1, -1

	if owner != "" {
		owningUser, err := user.Lookup(owner)
		if err != nil {
			return fmt.Errorf("unable to look up token file owner %s: %w", owner, err)
		}

		uid, _ = strconv.Atoi(owningUser.Uid)
		gid, _ = strconv.Atoi(owningUser.Gid)
	}

	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(tmp.Name()) }()

	// os.CreateTemp creates the file with mode 0600.
	_, err = tmp.WriteString(secret)
	closeErr := tmp.Close()

	err = errors.Join(err, closeErr)
	if err != nil {
		return err
	}

	if uid >= 0 {
		err = os.Chown(tmp.Name(), uid, gid)
		if err != nil {
			return err
		}
	}

	return os.Rename(tmp.Name(), path)
}

// consulServiceTokenACL implements serviceTokenACL on the Consul HTTP API,
// configured from the standard CONSUL_HTTP_* environment variables.
type consulServiceTokenACL struct {
	client *api.Client
}

func newConsulServiceTokenACL() (*consulServiceTokenACL, error) {
	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return nil, err
	}

	// Fail on a missing or wrong CONSUL_HTTP_TOKEN here, so that a later
	// "not found" always refers to the service token.
	_, _, err = client.ACL().TokenReadSelf(nil)
	if err != nil {
		if _, ok := errors.AsType[api.StatusError](err); ok {
			return nil, fmt.Errorf("CONSUL_HTTP_TOKEN is not a valid Consul token: %w", err)
		}

		return nil, fmt.Errorf("unable to reach Consul: %w", err)
	}

	return &consulServiceTokenACL{client: client}, nil
}

func (c *consulServiceTokenACL) NodeName() (string, error) {
	return c.client.Agent().NodeName()
}

func (c *consulServiceTokenACL) TokenRead(accessorID string) (*api.ACLToken, error) {
	token, _, err := c.client.ACL().TokenRead(accessorID, nil)
	if isTokenNotFound(err) {
		return nil, errServiceTokenNotFound
	}

	return token, err
}

func (c *consulServiceTokenACL) TokenCreate(token *api.ACLToken) (*api.ACLToken, error) {
	created, _, err := c.client.ACL().TokenCreate(token, nil)

	return created, err
}

func (c *consulServiceTokenACL) TokenUpdate(token *api.ACLToken) (*api.ACLToken, error) {
	updated, _, err := c.client.ACL().TokenUpdate(token, nil)

	return updated, err
}

func (c *consulServiceTokenACL) TokenListByPolicy(policyName string) ([]*api.ACLTokenListEntry, error) {
	policy, _, err := c.client.ACL().PolicyReadByName(policyName, nil)
	if err != nil {
		return nil, err
	}

	if policy == nil {
		return nil, nil
	}

	tokens, _, err := c.client.ACL().TokenListFiltered(api.ACLTokenFilterOptions{Policy: policy.ID}, nil)

	return tokens, err
}

func (c *consulServiceTokenACL) TokenDelete(accessorID string) error {
	_, err := c.client.ACL().TokenDelete(accessorID, nil)

	return err
}

// isTokenNotFound reports whether err is Consul's answer for a missing token:
// 404 on current versions, 403 "ACL not found" on older ones.
func isTokenNotFound(err error) bool {
	var statusErr api.StatusError
	if !errors.As(err, &statusErr) {
		return false
	}

	return statusErr.Code == 404 ||
		(statusErr.Code == 403 && strings.Contains(statusErr.Body, "ACL not found"))
}
