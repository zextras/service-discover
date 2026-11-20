// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package legacytoken revokes the per-service Consul ACL tokens that the packages of the services created before
// SPIRE (the pending-setups scripts). Once a node runs on SPIRE every service logs in the "spire" auth method and gets
// a token linked to its role, so the old tokens only stay valid for nothing.
package legacytoken

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/consul/api/v2"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/spire"
)

const (
	policySuffix = "-policy"
	roleSuffix   = "-role"
	// legacyDescriptionPrefix starts the description of the tokens created by the bash pending-setups:
	// "Token for <service>/<hostname -A>".
	legacyDescriptionPrefix = "Token for "
)

// ACL is the subset of the Consul ACL API needed to revoke the tokens.
type ACL interface {
	// TokenList returns every token of the cluster, as "consul acl token list" does.
	TokenList() ([]*api.ACLTokenListEntry, error)
	TokenDelete(accessorID string) error
}

// Selector tells which legacy tokens belong to one host. A token is attributed to a host only through its
// description, which is the single place where the host is recorded.
type Selector struct {
	// Hostnames are the names the host is known by, matched against the "Token for <service>/<hostnames>"
	// descriptions of the bash pending-setups (the names `hostname -A` returned). Matching is exact on each name.
	Hostnames []string
	// NodeName is the Consul node name of the host, matched against the "carbonio/<service>/<node>"
	// descriptions of the tokens created by "service-token ensure".
	NodeName string
	// ServiceRegistered, when set, tells whether SPIRE has the entry of the service for the host
	// (spiffe://<trust domain>/service/<service> under the node of the host). A token whose service is not
	// registered is kept. When nil the check is skipped.
	ServiceRegistered func(service string) (bool, error)
}

// NewSelector returns the selector of the host whose Consul node has the given role (command.Server or command.Agent).
func NewSelector(role command.ConsulRole, host string, serviceRegistered func(string) (bool, error)) Selector {
	return Selector{
		Hostnames:         []string{host},
		NodeName:          command.ConsulNodeName(role, host),
		ServiceRegistered: serviceRegistered,
	}
}

// Token is a legacy service token.
type Token struct {
	AccessorID  string `json:"accessor_id"`
	Service     string `json:"service"`
	Description string `json:"description"`
}

// Kept is a token attributed to the host that has not been revoked, and why.
type Kept struct {
	Token  Token  `json:"token"`
	Reason string `json:"reason"`
}

// Plan is what can be revoked for a host and what is kept.
type Plan struct {
	Revocable []Token `json:"revocable"`
	Kept      []Kept  `json:"kept"`
}

// Find returns the legacy service tokens of the host that can be revoked. A token is revocable when all the following
// hold, otherwise it is returned as kept (when it is attributed to the host) or ignored (when it belongs to someone
// else):
//   - it was not created by a login (no auth method) and is linked to the single policy "<service>-policy";
//   - its description names the host, as "Token for <service>/<hostnames>" or "carbonio/<service>/<node>";
//   - a token created by the "spire" auth method with the role "<service>-role" exists, so the service has a working
//     replacement (Consul does not record the host in a login token, so this is cluster wide);
//   - Selector.ServiceRegistered, if set, confirms that SPIRE has the service registered for the host.
func Find(acl ACL, selector Selector) (Plan, error) {
	tokens, err := acl.TokenList()
	if err != nil {
		return Plan{}, fmt.Errorf("unable to list the ACL tokens: %w", err)
	}

	loggedIn := loggedInServices(tokens)
	plan := Plan{}
	registered := map[string]bool{}

	for _, entry := range tokens {
		service, ok := candidateService(entry, selector)
		if !ok {
			continue
		}

		token := Token{AccessorID: entry.AccessorID, Service: service, Description: entry.Description}

		if !loggedIn[service] {
			plan.Kept = append(plan.Kept, Kept{token, "no token created by the " + spire.AuthMethodName +
				" auth method with the role " + service + roleSuffix + " exists yet: the service did not log in"})

			continue
		}

		if selector.ServiceRegistered != nil {
			known, seen := registered[service]
			if !seen {
				known, err = selector.ServiceRegistered(service)
				if err != nil {
					return Plan{}, fmt.Errorf("unable to check the SPIRE entry of %s: %w", service, err)
				}

				registered[service] = known
			}

			if !known {
				plan.Kept = append(plan.Kept, Kept{token, "SPIRE has no entry " + spire.ServiceID(service) +
					" for the host"})

				continue
			}
		}

		plan.Revocable = append(plan.Revocable, token)
	}

	return plan, nil
}

// Revoke deletes the revocable tokens of the plan, returning the ones deleted (all of them but the failed one and the
// following ones on error).
func (p Plan) Revoke(acl ACL) ([]Token, error) {
	var revoked []Token

	for _, token := range p.Revocable {
		err := acl.TokenDelete(token.AccessorID)
		if err != nil {
			return revoked, fmt.Errorf("unable to delete the ACL token %s of %s: %w", token.AccessorID, token.Service, err)
		}

		revoked = append(revoked, token)
	}

	return revoked, nil
}

// RevokeLegacyServiceTokens deletes the legacy service tokens of the host (see Find for what is deleted) and returns
// the deleted ones and the ones attributed to the host that were kept. It is idempotent.
func RevokeLegacyServiceTokens(acl ACL, selector Selector) ([]Token, []Kept, error) {
	plan, err := Find(acl, selector)
	if err != nil {
		return nil, nil, err
	}

	revoked, err := plan.Revoke(acl)

	return revoked, plan.Kept, err
}

// loggedInServices returns the services for which a token created by the SPIRE auth method exists.
func loggedInServices(tokens []*api.ACLTokenListEntry) map[string]bool {
	services := map[string]bool{}

	for _, entry := range tokens {
		if entry.AuthMethod != spire.AuthMethodName {
			continue
		}

		for _, role := range entry.Roles {
			if service, ok := strings.CutSuffix(role.Name, roleSuffix); ok && service != "" {
				services[service] = true
			}
		}
	}

	return services
}

// candidateService returns the service of a token that is a legacy service token of the host.
func candidateService(entry *api.ACLTokenListEntry, selector Selector) (string, bool) {
	if entry.AuthMethod != "" || len(entry.Roles) > 0 || len(entry.ServiceIdentities) > 0 ||
		len(entry.NodeIdentities) > 0 || len(entry.Policies) != 1 {
		return "", false
	}

	service, ok := strings.CutSuffix(entry.Policies[0].Name, policySuffix)
	if !ok || service == "" {
		return "", false
	}

	if !describesHost(entry.Description, service, selector) {
		return "", false
	}

	return service, true
}

// describesHost reports whether the description of a token of service names the host.
func describesHost(description, service string, selector Selector) bool {
	normalized := strings.Join(strings.Fields(description), " ")

	if selector.NodeName != "" && normalized == command.ServiceTokenDescription(service, selector.NodeName) {
		return true
	}

	legacy, found := strings.CutPrefix(normalized, legacyDescriptionPrefix)
	if !found {
		return false
	}

	name, hostnames, found := strings.Cut(legacy, "/")
	if !found || name != service {
		return false
	}

	for hostname := range strings.FieldsSeq(hostnames) {
		if slices.ContainsFunc(selector.Hostnames, func(known string) bool {
			return strings.EqualFold(known, strings.TrimSuffix(hostname, "."))
		}) {
			return true
		}
	}

	return false
}
