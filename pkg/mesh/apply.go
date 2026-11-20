// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/hashicorp/consul/api/v2"
	"github.com/zextras/service-discover/pkg/spire"
)

const (
	policyDescriptionPrefix      = "Carbonio mesh policy of "
	extraPolicyDescriptionPrefix = "Carbonio mesh additional rules of "
)

var errUnknownService = errors.New("unknown service")

// Cluster is the subset of the Consul API needed to register the definitions. Lookups return nil when the object
// does not exist.
type Cluster interface {
	PolicyByName(name string) (*api.ACLPolicy, error)
	PolicyCreate(policy *api.ACLPolicy) error
	PolicyUpdate(policy *api.ACLPolicy) error
	RoleByName(name string) (*api.ACLRole, error)
	RoleCreate(role *api.ACLRole) error
	RoleUpdate(role *api.ACLRole) error
	BindingRules(authMethod string) ([]*api.ACLBindingRule, error)
	BindingRuleCreate(rule *api.ACLBindingRule) error
	BindingRuleUpdate(rule *api.ACLBindingRule) error
	SetConfigEntry(entry api.ConfigEntry) error
}

// ApplyOptions tunes Apply.
type ApplyOptions struct {
	// TrustDomain is the SPIFFE trust domain of the mesh.
	TrustDomain string
	// AuthMethod is the name of the Consul auth method the binding rules belong to.
	AuthMethod string
	// Services restricts the registration to the named definitions, all of them when empty.
	Services []string
	// Out receives one line per change, nothing when nil.
	Out io.Writer
}

// Apply registers the definitions in Consul: for each one the ACL policy, the role linking it, the binding rule
// issuing the role to the JWT-SVID of the service and the config entries (protocol, intentions...). It is idempotent:
// objects that are already as defined are left untouched, and running it again after a package upgrade brings the
// cluster to the embedded definitions.
func Apply(cluster Cluster, defs []Definition, opts ApplyOptions) error {
	selected, err := selectDefinitions(defs, opts.Services)
	if err != nil {
		return err
	}

	var failures []error

	for i := range selected {
		err = applyDefinition(cluster, &selected[i], opts)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", selected[i].Name, err))
		}
	}

	return errors.Join(failures...)
}

// ApplyEmbedded registers the embedded definitions of the services (or only the named ones) with the given token,
// the bootstrap token, through the local Consul agent. It is the entry point for the setup of the first server and
// for the commands that have to bring a cluster to the definitions of this version.
func ApplyEmbedded(token, trustDomain string, services []string, out io.Writer) error {
	defs, err := Load()
	if err != nil {
		return err
	}

	cluster, err := NewCluster(token)
	if err != nil {
		return err
	}

	return Apply(cluster, defs, ApplyOptions{
		TrustDomain: trustDomain,
		AuthMethod:  spire.AuthMethodName,
		Services:    services,
		Out:         out,
	})
}

func selectDefinitions(defs []Definition, names []string) ([]Definition, error) {
	if len(names) == 0 {
		return defs, nil
	}

	selected := make([]Definition, 0, len(names))

	for _, name := range names {
		def, found := Find(defs, name)
		if !found {
			return nil, fmt.Errorf("%w: %s", errUnknownService, name)
		}

		selected = append(selected, *def)
	}

	return selected, nil
}

func applyDefinition(cluster Cluster, def *Definition, opts ApplyOptions) error {
	log := func(format string, args ...any) {
		if opts.Out != nil {
			_, _ = fmt.Fprintf(opts.Out, def.Name+": "+format+"\n", args...)
		}
	}

	policy, err := ensurePolicy(cluster, def, log)
	if err != nil {
		return err
	}

	policies := []string{policy.Name}

	extra, err := ensureExtraPolicy(cluster, def, log)
	if err != nil {
		return err
	}

	if extra != "" {
		policies = append(policies, extra)
	}

	err = ensureRole(cluster, def, policies, log)
	if err != nil {
		return err
	}

	err = ensureBindingRule(cluster, def, opts, log)
	if err != nil {
		return err
	}

	entries, err := def.ConfigEntries()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		err = cluster.SetConfigEntry(entry)
		if err != nil {
			return fmt.Errorf("unable to write the %s config entry %s: %w", entry.GetKind(), entry.GetName(), err)
		}
	}

	log("%d config entries written", len(entries))

	return nil
}

func ensurePolicy(cluster Cluster, def *Definition, log func(string, ...any)) (*api.ACLPolicy, error) {
	rules, err := def.Policy()
	if err != nil {
		return nil, err
	}

	return writePolicy(cluster, def.PolicyName(), policyDescriptionPrefix+def.Name, rules, log)
}

// ensureExtraPolicy writes the policy of the additional rules of the service and returns its name, empty when the
// service needs none.
func ensureExtraPolicy(cluster Cluster, def *Definition, log func(string, ...any)) (string, error) {
	rules, err := def.ExtraPolicy()
	if err != nil || rules == "" {
		return "", err
	}

	policy, err := writePolicy(cluster, def.ExtraPolicyName(), extraPolicyDescriptionPrefix+def.Name, rules, log)
	if err != nil {
		return "", err
	}

	return policy.Name, nil
}

func writePolicy(
	cluster Cluster,
	name, description, rules string,
	log func(string, ...any),
) (*api.ACLPolicy, error) {
	desired := &api.ACLPolicy{Name: name, Description: description, Rules: rules}

	existing, err := cluster.PolicyByName(desired.Name)
	if err != nil {
		return nil, fmt.Errorf("unable to read the policy %s: %w", desired.Name, err)
	}

	if existing == nil {
		err = cluster.PolicyCreate(desired)
		if err != nil {
			return nil, fmt.Errorf("unable to create the policy %s: %w", desired.Name, err)
		}

		log("policy %s created", desired.Name)

		return desired, nil
	}

	desired.ID = existing.ID

	// The rules are compared as written: Consul returns them verbatim.
	if existing.Rules == rules && existing.Description == desired.Description {
		return existing, nil
	}

	err = cluster.PolicyUpdate(desired)
	if err != nil {
		return nil, fmt.Errorf("unable to update the policy %s: %w", desired.Name, err)
	}

	log("policy %s updated", desired.Name)

	return desired, nil
}

func ensureRole(cluster Cluster, def *Definition, policies []string, log func(string, ...any)) error {
	desired := &api.ACLRole{
		Name:        def.RoleName(),
		Description: "Carbonio mesh role of " + def.Name,
	}

	for _, name := range policies {
		desired.Policies = append(desired.Policies, &api.ACLRolePolicyLink{Name: name})
	}

	existing, err := cluster.RoleByName(desired.Name)
	if err != nil {
		return fmt.Errorf("unable to read the role %s: %w", desired.Name, err)
	}

	if existing == nil {
		err = cluster.RoleCreate(desired)
		if err != nil {
			return fmt.Errorf("unable to create the role %s: %w", desired.Name, err)
		}

		log("role %s created", desired.Name)

		return nil
	}

	linked := make([]string, 0, len(existing.Policies))
	for _, link := range existing.Policies {
		linked = append(linked, link.Name)
	}

	slices.Sort(linked)

	wanted := slices.Sorted(slices.Values(policies))

	if slices.Equal(linked, wanted) && len(existing.ServiceIdentities) == 0 && len(existing.NodeIdentities) == 0 &&
		len(existing.TemplatedPolicies) == 0 && existing.Description == desired.Description {
		return nil
	}

	desired.ID = existing.ID

	err = cluster.RoleUpdate(desired)
	if err != nil {
		return fmt.Errorf("unable to update the role %s: %w", desired.Name, err)
	}

	log("role %s updated", desired.Name)

	return nil
}

func ensureBindingRule(cluster Cluster, def *Definition, opts ApplyOptions, log func(string, ...any)) error {
	desired := &api.ACLBindingRule{
		Description: "Carbonio mesh: JWT-SVID of " + def.Name,
		AuthMethod:  opts.AuthMethod,
		Selector:    def.Selector(opts.TrustDomain),
		BindType:    api.BindingRuleBindTypeRole,
		BindName:    def.RoleName(),
	}

	rules, err := cluster.BindingRules(opts.AuthMethod)
	if err != nil {
		return fmt.Errorf("unable to list the binding rules of %s: %w", opts.AuthMethod, err)
	}

	// The rule of a service is the one binding its role: a trust domain change updates the selector in place.
	found := slices.IndexFunc(rules, func(r *api.ACLBindingRule) bool {
		return r.BindType == api.BindingRuleBindTypeRole && r.BindName == desired.BindName
	})

	if found < 0 {
		err = cluster.BindingRuleCreate(desired)
		if err != nil {
			return fmt.Errorf("unable to create the binding rule of %s: %w", def.Name, err)
		}

		log("binding rule created: %s", desired.Selector)

		return nil
	}

	if rules[found].Selector == desired.Selector && rules[found].Description == desired.Description {
		return nil
	}

	desired.ID = rules[found].ID

	err = cluster.BindingRuleUpdate(desired)
	if err != nil {
		return fmt.Errorf("unable to update the binding rule of %s: %w", def.Name, err)
	}

	log("binding rule updated: %s", desired.Selector)

	return nil
}

// kindRank orders the config entries so that the service protocol exists before the L7 intentions referring to it.
func kindRank(kind string) int {
	switch kind {
	case api.ServiceDefaults:
		return 0
	case api.ServiceRouter:
		return 1
	default:
		return 2
	}
}
