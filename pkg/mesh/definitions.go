// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package mesh owns the service mesh definitions of the Carbonio services and registers them centrally.
//
// The definitions (ACL policy, service protocol and intentions of every service) used to be applied by the pending
// setup script of each package, with the cluster bootstrap token. They are data now, shipped by the
// service-discover-specs package: the cluster applies them once, with the bootstrap token, on the servers ("mesh
// apply"), and every node obtains the credentials of its services from SPIRE ("service sync"). The packages of the
// services are not involved anymore, and a new service needs a release of the specs, not of service-discover.
package mesh

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/hashicorp/consul/api/v2"
)

const (
	indexFile    = "index.json"
	policiesFile = "policies.json"

	// PendingSetupReplace marks the services whose pending setup script is entirely replaced by the central
	// registration: it has nothing to do besides the mesh and the token. PendingSetupKeep marks the ones whose script
	// also does something else, that cannot run without the bootstrap token.
	PendingSetupReplace = "replace"
	PendingSetupKeep    = "keep"
)

var (
	errNoPolicies   = errors.New("definition has no policies.json")
	errDuplicate    = errors.New("duplicate definition")
	errInvalidName  = errors.New("invalid definition")
	errMissingField = errors.New("name, dir and user are required")
	errPendingSetup = errors.New("unknown pending_setup")
	errUnknownStep  = errors.New("unknown setup step")
)

// SourceFile is a file of a definition together with the package repository it has been copied from, so that the
// definitions can be refreshed mechanically (hack/sync.sh of service-discover-specs).
type SourceFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// TokenEnv is a systemd unit reading the service token from an environment variable of a drop-in the package setup
// script used to generate.
type TokenEnv struct {
	Unit string `json:"unit"`
	Var  string `json:"var"`
}

// Definition is everything the mesh needs to know about one service identity: the ACL policy, the service defaults
// and the intentions it is registered with, the user it runs as and the units to refresh when its token changes. A
// definition is one token and one SPIFFE ID, so a definition may cover more than one Consul service (the message
// dispatcher registers three, all with the same token).
type Definition struct {
	// Name is the service name, e.g. carbonio-files. It is the last element of the SPIFFE ID, and the prefix of the
	// ACL policy and role names.
	Name string `json:"name"`
	// Dir is the directory under /etc/carbonio holding the "service-discover" directory of the service.
	Dir string `json:"dir"`
	// User is the operating system user the service runs as, and the owner of its token.
	User string `json:"user"`
	// HCL lists the Consul service definition files the package installs in /etc/zextras/service-discover.
	HCL []string `json:"hcl"`
	// Sidecars lists the units of the Connect sidecar proxies, reading the token file.
	Sidecars []string `json:"sidecars"`
	// Restart lists the other units the package setup script restarted after writing the token.
	Restart []string `json:"restart"`
	// TokenEnv lists the units getting the token through an environment variable.
	TokenEnv []TokenEnv `json:"token_env"`
	// Generated tells that the package does not ship the HCL files: its setup generates them. They are not on the
	// disk before the setup, so the service counts as installed when its service-discover directory is.
	Generated bool `json:"generated,omitempty"`
	// Companions lists the other definitions the same pending setup script sets up: they are synchronized together,
	// after this one.
	Companions []string `json:"companions,omitempty"`
	// Configure lists the commands (name and arguments) that prepare the configuration of the service and, for the
	// Generated ones, produce the HCL files. They run when the service is set up, before the definitions are
	// rendered, as root with the token of the service in the environment (CONSUL_HTTP_TOKEN and SETUP_CONSUL_TOKEN)
	// and LOCAL_IP set.
	Configure []string `json:"configure,omitempty"`
	// Routing lists the commands that write the service router and resolver of the service, run like Configure once
	// the service is registered.
	Routing []string `json:"routing,omitempty"`
	// Setup names a step of the setup implemented here, see the steps table.
	Setup string `json:"setup,omitempty"`
	// Migrations are the changes of the configuration of the service its upgrades need.
	Migrations Migrations `json:"migrations"`
	// SubstituteFiles lists, relative to /etc/carbonio, the configuration files carrying the placeholders
	// {{ consultoken }}, {{ hostsdomain }} and {{ consulhostsdomain }}, that the setup of the package replaced in place.
	SubstituteFiles []string `json:"substitute_files,omitempty"`
	// ExtraRules are ACL rules the service needs beyond the ones of its package policy, in the format of the
	// policies; see ExtraPolicy.
	ExtraRules Rules `json:"extra_rules,omitempty"`
	// Comment documents the choices made for the service that the files do not show.
	Comment string `json:"comment,omitempty"`
	// PostSetup lists the commands the package setup script runs after the mesh part. They only need the service
	// token.
	PostSetup []string `json:"post_setup"`
	// PendingSetup is PendingSetupReplace or PendingSetupKeep.
	PendingSetup string `json:"pending_setup"`
	// ConsumerOnly marks services holding a token to read the catalog but registering nothing.
	ConsumerOnly bool `json:"consumer_only"`
	// Repo is the package repository the files have been copied from, AltRepos are other repositories shipping the
	// same service (e.g. the community edition) that have been compared and not copied.
	Repo     string       `json:"repo"`
	AltRepos []string     `json:"alt_repos"`
	Files    []SourceFile `json:"files"`

	fsys fs.FS
}

// index is the content of index.json.
type index struct {
	Services []Definition `json:"services"`
}

// LoadFS reads definitions from a directory holding index.json and one directory per definition.
func LoadFS(fsys fs.FS) ([]Definition, error) {
	raw, err := fs.ReadFile(fsys, indexFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read the definitions index: %w", err)
	}

	idx := index{}

	err = json.Unmarshal(raw, &idx)
	if err != nil {
		return nil, fmt.Errorf("unable to decode the definitions index: %w", err)
	}

	seen := map[string]bool{}

	for i := range idx.Services {
		def := &idx.Services[i]
		def.fsys = fsys

		err = def.validate()
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", errInvalidName, def.Name, err)
		}

		if seen[def.Name] {
			return nil, fmt.Errorf("%w %q", errDuplicate, def.Name)
		}

		seen[def.Name] = true
	}

	slices.SortFunc(idx.Services, func(a, b Definition) int { return strings.Compare(a.Name, b.Name) })

	return idx.Services, nil
}

// PolicyName is the name of the ACL policy of the service. It is the one the package setup scripts used.
func (d *Definition) PolicyName() string {
	return d.Name + "-policy"
}

// RoleName is the name of the ACL role bound to the SPIFFE ID of the service.
func (d *Definition) RoleName() string {
	return d.Name + "-role"
}

// SpiffeID is the SPIFFE ID of the service in the given trust domain.
func (d *Definition) SpiffeID(trustDomain string) string {
	return "spiffe://" + trustDomain + "/service/" + d.Name
}

// Selector is the binding rule selector matching the JWT-SVID of the service.
func (d *Definition) Selector(trustDomain string) string {
	return `value.spiffe_id == "` + d.SpiffeID(trustDomain) + `"`
}

// UnixSelector is the SPIRE selector matching the processes of the service.
func (d *Definition) UnixSelector() string {
	return "unix:user:" + d.User
}

// ServiceDirectory is the directory the package installs the mesh files of the service in.
func (d *Definition) ServiceDirectory() string {
	return path.Join(CarbonioDir, d.Dir, "service-discover")
}

// TokenFile is the file holding the token of the service, read by its sidecars.
func (d *Definition) TokenFile() string {
	return path.Join(d.ServiceDirectory(), "token")
}

// ExtraPolicyName is the name of the ACL policy holding the rules the service needs beyond its package policy.
func (d *Definition) ExtraPolicyName() string {
	return d.Name + "-extra-policy"
}

// ExtraPolicy returns the rules the service needs beyond the policy of its package, empty when there are none. The
// package policy is the one the package setup scripts keep writing under the same name: the additions are in a
// policy of their own, linked to the role, so that they are not lost when a script runs on a node outside of SPIRE.
// They are the rules written in the definition, and the write access to the keys the migrations of the service move:
// a migration deletes the old key, which the read access of the package policy does not allow, and writes the new
// one.
//
// Every service registering something also reads the catalog: the token of the service is the one its registration
// carries, and Consul resolves the upstreams of its sidecar with it. Without the read access to the upstream services
// the sidecar gets no upstream listener at all, and the service cannot reach its databases and the other services.
// The package policies were written when the registration had no token and the agent resolved the upstreams. Who may
// connect to whom is up to the intentions, the write access stays limited to the service itself.
func (d *Definition) ExtraPolicy() (string, error) {
	rules := Rules{}

	for kind, list := range d.ExtraRules {
		rules[kind] = slices.Clone(list)
	}

	if len(d.HCL) > 0 && !readsAllServices(rules) {
		rules["service_prefix"] = append(rules["service_prefix"], map[string]Rule{"": {Policy: policyRead}})
	}

	for _, key := range d.Migrations.Keys() {
		rules["key"] = append(rules["key"], map[string]Rule{key: {Policy: "write"}})
	}

	if len(rules) == 0 {
		return "", nil
	}

	raw, err := json.MarshalIndent(rules, "", "  ")

	return string(raw), err
}

// policyRead is the ACL access that reads without changing.
const policyRead = "read"

// Rules are ACL rules in the JSON format of the policies: for each resource kind, a list of maps from the resource to
// the rule.
type Rules map[string][]map[string]Rule

// Rule is the access to a resource.
type Rule struct {
	Policy string `json:"policy"`
}

// readsAllServices tells whether the rules already give at least the read access to every service.
func readsAllServices(rules Rules) bool {
	for _, list := range rules["service_prefix"] {
		if rule, found := list[""]; found && (rule.Policy == policyRead || rule.Policy == "write") {
			return true
		}
	}

	return false
}

// Policy returns the rules of the ACL policy of the service.
func (d *Definition) Policy() (string, error) {
	raw, err := fs.ReadFile(d.fsys, path.Join(d.Name, policiesFile))
	if err != nil {
		return "", fmt.Errorf("%w: %w", errNoPolicies, err)
	}

	return string(raw), nil
}

// ConfigEntries returns the Consul config entries of the service: service defaults (protocol), intentions and, for
// the services needing one, the service router. The service protocol comes first, as the intentions may depend on it.
func (d *Definition) ConfigEntries() ([]api.ConfigEntry, error) {
	var entries []api.ConfigEntry

	for _, file := range d.Files {
		if file.Name == policiesFile {
			continue
		}

		raw, err := fs.ReadFile(d.fsys, path.Join(d.Name, file.Name))
		if err != nil {
			return nil, err
		}

		entry, err := api.DecodeConfigEntryFromJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name, err)
		}

		entries = append(entries, entry)
	}

	slices.SortStableFunc(entries, func(a, b api.ConfigEntry) int { return kindRank(a.GetKind()) - kindRank(b.GetKind()) })

	return entries, nil
}

func (d *Definition) validate() error {
	if d.Name == "" || d.Dir == "" || d.User == "" {
		return errMissingField
	}

	if d.PendingSetup != PendingSetupReplace && d.PendingSetup != PendingSetupKeep {
		return fmt.Errorf("%w: %q", errPendingSetup, d.PendingSetup)
	}

	if d.Setup != "" {
		if _, known := steps[d.Setup]; !known {
			return fmt.Errorf("%w: %q", errUnknownStep, d.Setup)
		}
	}

	_, err := d.Policy()
	if err != nil {
		return err
	}

	_, err = d.ExtraPolicy()
	if err != nil {
		return err
	}

	_, err = d.ConfigEntries()

	return err
}

// Find returns the definition with the given name.
func Find(defs []Definition, name string) (*Definition, bool) {
	i := slices.IndexFunc(defs, func(d Definition) bool { return d.Name == name })
	if i < 0 {
		return nil, false
	}

	return &defs[i], true
}
