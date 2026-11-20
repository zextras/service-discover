// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"strconv"
	"testing"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCluster is an in-memory Consul.
type fakeCluster struct {
	policies map[string]*api.ACLPolicy
	roles    map[string]*api.ACLRole
	rules    []*api.ACLBindingRule
	entries  map[string]api.ConfigEntry
	writes   int
	failKind string
}

var errCluster = errors.New("consul is down")

func newFakeCluster() *fakeCluster {
	return &fakeCluster{
		policies: map[string]*api.ACLPolicy{},
		roles:    map[string]*api.ACLRole{},
		entries:  map[string]api.ConfigEntry{},
	}
}

func (f *fakeCluster) PolicyByName(name string) (*api.ACLPolicy, error) { return f.policies[name], nil }

func (f *fakeCluster) PolicyCreate(policy *api.ACLPolicy) error {
	f.writes++
	policy.ID = "policy-" + strconv.Itoa(len(f.policies))
	f.policies[policy.Name] = policy

	return nil
}

func (f *fakeCluster) PolicyUpdate(policy *api.ACLPolicy) error {
	f.writes++
	f.policies[policy.Name] = policy

	return nil
}

func (f *fakeCluster) RoleByName(name string) (*api.ACLRole, error) { return f.roles[name], nil }

func (f *fakeCluster) RoleCreate(role *api.ACLRole) error {
	f.writes++
	role.ID = "role-" + strconv.Itoa(len(f.roles))
	f.roles[role.Name] = role

	return nil
}

func (f *fakeCluster) RoleUpdate(role *api.ACLRole) error {
	f.writes++
	f.roles[role.Name] = role

	return nil
}

func (f *fakeCluster) BindingRules(string) ([]*api.ACLBindingRule, error) { return f.rules, nil }

func (f *fakeCluster) BindingRuleCreate(rule *api.ACLBindingRule) error {
	f.writes++
	rule.ID = "rule-" + strconv.Itoa(len(f.rules))
	f.rules = append(f.rules, rule)

	return nil
}

func (f *fakeCluster) BindingRuleUpdate(rule *api.ACLBindingRule) error {
	f.writes++

	for i, existing := range f.rules {
		if existing.ID == rule.ID {
			f.rules[i] = rule
		}
	}

	return nil
}

func (f *fakeCluster) SetConfigEntry(entry api.ConfigEntry) error {
	if entry.GetKind() == f.failKind {
		return errCluster
	}

	f.entries[entry.GetKind()+"/"+entry.GetName()] = entry

	return nil
}

func TestApply_registersEveryDefinition(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	cluster := newFakeCluster()
	out := &bytes.Buffer{}

	require.NoError(t, Apply(cluster, defs, ApplyOptions{TrustDomain: "carbonio.lab", AuthMethod: "spire", Out: out}))

	assert.Len(t, cluster.policies, len(defs))
	assert.Len(t, cluster.roles, len(defs))
	assert.Len(t, cluster.rules, len(defs))
	assert.NotEmpty(t, cluster.entries)

	files, _ := Find(defs, "carbonio-files")

	rule := findRule(t, cluster, "carbonio-files-role")
	assert.Equal(t, `value.spiffe_id == "spiffe://carbonio.lab/service/carbonio-files"`, rule.Selector)
	assert.Equal(t, api.BindingRuleBindTypeRole, rule.BindType)
	assert.Equal(t, "spire", rule.AuthMethod)

	role := cluster.roles[files.RoleName()]
	require.Len(t, role.Policies, 1)
	assert.Equal(t, files.PolicyName(), role.Policies[0].Name)

	rules, _ := files.Policy()
	assert.Equal(t, rules, cluster.policies[files.PolicyName()].Rules)

	assert.Contains(t, cluster.entries, "service-defaults/carbonio-files")
	assert.Contains(t, cluster.entries, "service-intentions/carbonio-files")
	assert.Contains(t, out.String(), "carbonio-files: policy carbonio-files-policy created")
}

func findRule(t *testing.T, cluster *fakeCluster, bindName string) *api.ACLBindingRule {
	t.Helper()

	for _, rule := range cluster.rules {
		if rule.BindName == bindName {
			return rule
		}
	}

	require.Failf(t, "binding rule not found", bindName)

	return nil
}

func TestApply_isIdempotent(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	cluster := newFakeCluster()
	opts := ApplyOptions{TrustDomain: "carbonio", AuthMethod: "spire"}

	require.NoError(t, Apply(cluster, defs, opts))

	writes := cluster.writes

	out := &bytes.Buffer{}
	opts.Out = out

	require.NoError(t, Apply(cluster, defs, opts))
	assert.Equal(t, writes, cluster.writes, "nothing is rewritten when the cluster is up to date")
	assert.NotContains(t, out.String(), "created")
	assert.NotContains(t, out.String(), "updated")
}

func TestApply_repairsDrift(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	cluster := newFakeCluster()
	opts := ApplyOptions{TrustDomain: "carbonio", AuthMethod: "spire", Services: []string{"carbonio-files"}}

	require.NoError(t, Apply(cluster, defs, opts))
	require.Len(t, cluster.policies, 1)

	cluster.policies["carbonio-files-policy"].Rules = `key_prefix "" { policy = "write" }`
	cluster.roles["carbonio-files-role"].Policies = nil
	findRule(t, cluster, "carbonio-files-role").Selector = `value.spiffe_id == "spiffe://other/service/x"`

	out := &bytes.Buffer{}
	opts.Out = out

	require.NoError(t, Apply(cluster, defs, opts))

	files, _ := Find(defs, "carbonio-files")
	rules, _ := files.Policy()

	assert.Equal(t, rules, cluster.policies["carbonio-files-policy"].Rules)
	assert.Equal(t, "carbonio-files-policy", cluster.roles["carbonio-files-role"].Policies[0].Name)
	assert.Equal(t, `value.spiffe_id == "spiffe://carbonio/service/carbonio-files"`,
		findRule(t, cluster, "carbonio-files-role").Selector)
	assert.Len(t, cluster.rules, 1, "the rule is updated, not duplicated")
	assert.Contains(t, out.String(), "policy carbonio-files-policy updated")
	assert.Contains(t, out.String(), "role carbonio-files-role updated")
	assert.Contains(t, out.String(), "binding rule updated")
}

func TestApply_selectsServices(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	cluster := newFakeCluster()

	require.NoError(t, Apply(cluster, defs, ApplyOptions{
		TrustDomain: "carbonio", AuthMethod: "spire", Services: []string{"carbonio-files", "carbonio-preview"},
	}))
	assert.Len(t, cluster.policies, 2)

	err = Apply(cluster, defs, ApplyOptions{TrustDomain: "carbonio", AuthMethod: "spire", Services: []string{"nope"}})
	require.ErrorIs(t, err, errUnknownService)
}

func TestApply_continuesAfterAFailureAndReportsIt(t *testing.T) {
	t.Parallel()

	defs, err := Load()
	require.NoError(t, err)

	cluster := newFakeCluster()
	cluster.failKind = api.ServiceRouter

	err = Apply(cluster, defs, ApplyOptions{TrustDomain: "carbonio", AuthMethod: "spire"})
	require.ErrorIs(t, err, errCluster)
	require.ErrorContains(t, err, "carbonio-files")
	assert.Len(t, cluster.policies, len(defs), "the other services are registered")
}
