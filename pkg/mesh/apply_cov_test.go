// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyFailingCluster fails the operations named in fail, either as "Operation" or as "Operation:object name".
type applyFailingCluster struct {
	*fakeCluster

	fail map[string]bool
}

func (f *applyFailingCluster) failing(op, name string) error {
	if f.fail[op] || f.fail[op+":"+name] {
		return errCluster
	}

	return nil
}

func (f *applyFailingCluster) PolicyByName(name string) (*api.ACLPolicy, error) {
	if err := f.failing("PolicyByName", name); err != nil {
		return nil, err
	}

	return f.fakeCluster.PolicyByName(name)
}

func (f *applyFailingCluster) PolicyCreate(policy *api.ACLPolicy) error {
	if err := f.failing("PolicyCreate", policy.Name); err != nil {
		return err
	}

	return f.fakeCluster.PolicyCreate(policy)
}

func (f *applyFailingCluster) PolicyUpdate(policy *api.ACLPolicy) error {
	if err := f.failing("PolicyUpdate", policy.Name); err != nil {
		return err
	}

	return f.fakeCluster.PolicyUpdate(policy)
}

func (f *applyFailingCluster) RoleByName(name string) (*api.ACLRole, error) {
	if err := f.failing("RoleByName", name); err != nil {
		return nil, err
	}

	return f.fakeCluster.RoleByName(name)
}

func (f *applyFailingCluster) RoleCreate(role *api.ACLRole) error {
	if err := f.failing("RoleCreate", role.Name); err != nil {
		return err
	}

	return f.fakeCluster.RoleCreate(role)
}

func (f *applyFailingCluster) RoleUpdate(role *api.ACLRole) error {
	if err := f.failing("RoleUpdate", role.Name); err != nil {
		return err
	}

	return f.fakeCluster.RoleUpdate(role)
}

func (f *applyFailingCluster) BindingRules(authMethod string) ([]*api.ACLBindingRule, error) {
	if err := f.failing("BindingRules", authMethod); err != nil {
		return nil, err
	}

	return f.fakeCluster.BindingRules(authMethod)
}

func (f *applyFailingCluster) BindingRuleCreate(rule *api.ACLBindingRule) error {
	if err := f.failing("BindingRuleCreate", rule.BindName); err != nil {
		return err
	}

	return f.fakeCluster.BindingRuleCreate(rule)
}

func (f *applyFailingCluster) BindingRuleUpdate(rule *api.ACLBindingRule) error {
	if err := f.failing("BindingRuleUpdate", rule.BindName); err != nil {
		return err
	}

	return f.fakeCluster.BindingRuleUpdate(rule)
}

func (f *applyFailingCluster) ConfigEntry(kind, name string) (api.ConfigEntry, error) {
	if err := f.failing("ConfigEntry", name); err != nil {
		return nil, err
	}

	return f.fakeCluster.ConfigEntry(kind, name)
}

func (f *applyFailingCluster) SetConfigEntry(entry api.ConfigEntry) error {
	if err := f.failing("SetConfigEntry", entry.GetName()); err != nil {
		return err
	}

	return f.fakeCluster.SetConfigEntry(entry)
}

func applyFilesOptions(out *bytes.Buffer) ApplyOptions {
	opts := ApplyOptions{TrustDomain: "carbonio.lab", AuthMethod: "spire", Services: []string{"carbonio-files"}}
	if out != nil {
		opts.Out = out
	}

	return opts
}

func TestApply_reportsEveryClusterFailure(t *testing.T) {
	t.Parallel()

	const (
		extra = "carbonio-files-extra-policy"
		role  = "carbonio-files-role"
	)

	tests := []struct {
		name    string
		fail    string
		drift   func(cluster *fakeCluster)
		wantErr string
	}{
		{"policy lookup", "PolicyByName", nil, "unable to read the policy carbonio-files-policy"},
		{"policy creation", "PolicyCreate", nil, "unable to create the policy carbonio-files-policy"},
		{"extra policy lookup", "PolicyByName:" + extra, nil, "unable to read the policy " + extra},
		{"extra policy creation", "PolicyCreate:" + extra, nil, "unable to create the policy " + extra},
		{
			"policy update", "PolicyUpdate",
			func(c *fakeCluster) { c.policies["carbonio-files-policy"].Rules = "stale" },
			"unable to update the policy carbonio-files-policy",
		},
		{
			"extra policy update", "PolicyUpdate:" + extra,
			func(c *fakeCluster) { c.policies[extra].Rules = "stale" },
			"unable to update the policy " + extra,
		},
		{"role lookup", "RoleByName", nil, "unable to read the role " + role},
		{"role creation", "RoleCreate", nil, "unable to create the role " + role},
		{
			"role update", "RoleUpdate",
			func(c *fakeCluster) { c.roles[role].Description = "stale" },
			"unable to update the role " + role,
		},
		{"binding rules listing", "BindingRules", nil, "unable to list the binding rules of spire"},
		{"binding rule creation", "BindingRuleCreate", nil, "unable to create the binding rule of carbonio-files"},
		{
			"binding rule update", "BindingRuleUpdate",
			func(c *fakeCluster) { c.rules[0].Selector = "stale" },
			"unable to update the binding rule of carbonio-files",
		},
		{"config entry lookup", "ConfigEntry", nil, "unable to read the service-defaults config entry"},
		{"config entry write", "SetConfigEntry", nil, "unable to write the service-defaults config entry"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defs, err := Load()
			require.NoError(t, err)

			cluster := newFakeCluster()

			if tt.drift != nil {
				require.NoError(t, Apply(cluster, defs, applyFilesOptions(nil)))
				tt.drift(cluster)
			}

			failing := &applyFailingCluster{fakeCluster: cluster, fail: map[string]bool{tt.fail: true}}

			err = Apply(failing, defs, applyFilesOptions(&bytes.Buffer{}))
			require.ErrorIs(t, err, errCluster)
			require.ErrorContains(t, err, "carbonio-files: ")
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestApply_brokenDefinitions(t *testing.T) {
	t.Parallel()

	const defaults = `{"Kind":"service-defaults","Name":"x","Protocol":"http"}`

	tests := []struct {
		name    string
		files   []SourceFile
		fsys    fstest.MapFS
		wantErr error
	}{
		{"no policy file", nil, fstest.MapFS{}, errNoPolicies},
		{
			"missing config entry file", []SourceFile{{Name: "gone.json"}},
			fstest.MapFS{"x/policies.json": {Data: []byte("{}")}}, fs.ErrNotExist,
		},
		{
			"invalid config entry", []SourceFile{{Name: "bad.json"}},
			fstest.MapFS{"x/policies.json": {Data: []byte("{}")}, "x/bad.json": {Data: []byte(`{"Kind":"nonsense"}`)}},
			nil,
		},
		{
			"valid", []SourceFile{{Name: "policies.json"}, {Name: "defaults.json"}},
			fstest.MapFS{"x/policies.json": {Data: []byte("{}")}, "x/defaults.json": {Data: []byte(defaults)}}, applyNoError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			def := Definition{Name: "x", Dir: "x", User: "u", Files: tt.files, fsys: tt.fsys}
			cluster := newFakeCluster()

			err := Apply(cluster, []Definition{def}, ApplyOptions{TrustDomain: "carbonio.lab", AuthMethod: "spire"})

			switch {
			case tt.wantErr == applyNoError:
				require.NoError(t, err)
				assert.Contains(t, cluster.entries, "service-defaults/x")
				assert.NotContains(t, cluster.policies, "x-extra-policy", "no extra policy without rules")
			case tt.wantErr == nil:
				require.ErrorContains(t, err, "x: bad.json")
			default:
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorContains(t, err, "x: ")
			}
		})
	}
}

func TestApplySpecs(t *testing.T) {
	replies := map[string]meshReply{
		meshTokenSelf:               {meshOK, `{}`},
		"PUT /v1/acl/policy":        {meshOK, `{"ID":"p"}`},
		"PUT /v1/acl/role":          {meshOK, `{"ID":"r"}`},
		"GET /v1/acl/binding-rules": {meshOK, `[]`},
		"PUT /v1/acl/binding-rule":  {meshOK, `{"ID":"b"}`},
		"PUT /v1/config":            {meshOK, `true`},
	}

	t.Run("registers the service", func(t *testing.T) {
		stub := newMeshConsul(t, replies)
		out := &bytes.Buffer{}

		require.NoError(t, ApplySpecs("boot", "carbonio.lab", []string{"carbonio-files"}, out))

		assert.Contains(t, out.String(), "carbonio-files: policy carbonio-files-policy created")
		assert.Contains(t, out.String(), "carbonio-files: role carbonio-files-role created")
		assert.Contains(t, out.String(), "config entries: ")
		assert.Equal(t, "boot", stub.last(t).token)

		var created map[string]any

		stub.mu.Lock()
		for _, req := range stub.requests {
			if req.method == "PUT" && req.path == "/v1/acl/binding-rule" {
				require.NoError(t, json.Unmarshal([]byte(req.body), &created))
			}
		}
		stub.mu.Unlock()

		assert.Equal(t, `value.spiffe_id == "spiffe://carbonio.lab/service/carbonio-files"`, created["Selector"])
		assert.Equal(t, "spire", created["AuthMethod"])
	})

	t.Run("unknown service", func(t *testing.T) {
		newMeshConsul(t, replies)

		require.ErrorIs(t, ApplySpecs("boot", "carbonio.lab", []string{"nonexistent"}, nil), errUnknownService)
	})

	t.Run("invalid token", func(t *testing.T) {
		newMeshConsul(t, map[string]meshReply{meshTokenSelf: {403, "ACL not found"}})

		require.ErrorContains(t, ApplySpecs("boot", "carbonio.lab", nil, nil), "the Consul token is not valid")
	})
}

// applyNoError marks the cases that must succeed.
var applyNoError = errors.New("no error expected")

// applyUnmarshalable cannot be encoded: its fields cannot be compared.
type applyUnmarshalable struct {
	api.ConfigEntry

	Broken chan int
}

// applyScalar encodes as a JSON number, not as the object of a config entry.
type applyScalar struct{ api.ConfigEntry }

func (applyScalar) MarshalJSON() ([]byte, error) { return []byte("1"), nil }

func TestSameConfigEntry_uncomparableEntriesDiffer(t *testing.T) {
	t.Parallel()

	good := &api.ServiceConfigEntry{Kind: api.ServiceDefaults, Name: "x"}

	assert.False(t, sameConfigEntry(good, &applyUnmarshalable{}))
	assert.False(t, sameConfigEntry(&applyUnmarshalable{}, good))
	assert.False(t, sameConfigEntry(good, applyScalar{}))
	assert.True(t, sameConfigEntry(good, good))
}

func TestSameValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		want, have any
		same       bool
	}{
		{"equal scalars", "a", "a", true},
		{"different scalars", "a", "b", false},
		{"empty scalars of different kinds", "", nil, true},
		{"object and a scalar", map[string]any{"A": "x"}, "x", false},
		{"empty object and nothing", map[string]any{}, nil, true},
		{"list and a scalar", []any{"x"}, "x", false},
		{"empty list and nothing", []any{}, nil, true},
		{"wanted field missing", map[string]any{"A": "x"}, map[string]any{}, false},
		{"wanted empty field missing", map[string]any{"A": ""}, map[string]any{}, true},
		{"extra stored field", map[string]any{}, map[string]any{"B": "x"}, false},
		{"extra empty stored field", map[string]any{}, map[string]any{"B": ""}, true},
		{"extra field set by Consul", map[string]any{}, map[string]any{"ModifyIndex": 12.0}, true},
		{"wanted field set by Consul is ignored", map[string]any{"Hash": "x"}, map[string]any{"Hash": "y"}, true},
		{"nested difference", map[string]any{"A": map[string]any{"B": "x"}}, map[string]any{"A": map[string]any{"B": "y"}}, false},
		{"lists in another order", []any{"a", "b"}, []any{"b", "a"}, true},
		{"lists of another length", []any{"a"}, []any{"a", "b"}, false},
		{"lists with another item", []any{"a", "b"}, []any{"a", "c"}, false},
		{"list items are used once", []any{"a", "a"}, []any{"a", "b"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.same, sameValue(tt.want, tt.have))
		})
	}
}

func TestIsEmptyValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		empty bool
	}{
		{"nil", nil, true},
		{"empty string", "", true},
		{"string", "x", false},
		{"false", false, true},
		{"true", true, false},
		{"zero", 0.0, true},
		{"number", 2.0, false},
		{"empty list", []any{}, true},
		{"list", []any{1.0}, false},
		{"empty object", map[string]any{}, true},
		{"object", map[string]any{"a": 1.0}, false},
		{"a type JSON does not produce", 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.empty, isEmptyValue(tt.value))
		})
	}
}
