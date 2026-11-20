// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package mesh owns the service mesh definitions of the Carbonio services and registers them centrally.
//
// The definitions (ACL policy, service protocol and intentions of every service) used to be applied by the pending
// setup script of each package, with the cluster bootstrap token. They are embedded here instead: the cluster applies
// them once, with the bootstrap token, on the servers ("mesh apply"), and every node obtains the credentials of its
// services from SPIRE ("service sync"). The packages are not involved anymore.
package mesh

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/hashicorp/consul/api/v2"
)

//go:embed definitions
var embedded embed.FS

const (
	definitionsDir = "definitions"
	indexFile      = "index.json"
	policiesFile   = "policies.json"

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
)

// SourceFile is a file of a definition together with the package repository it has been copied from, so that the
// embedded copy can be refreshed mechanically (hack/sync-mesh-definitions.sh).
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
	// PostSetup lists the commands the package setup script runs after the mesh part. They only need the service
	// token.
	PostSetup []string `json:"post_setup"`
	// PendingSetup is PendingSetupReplace or PendingSetupKeep.
	PendingSetup string `json:"pending_setup"`
	// ConsumerOnly marks services holding a token to read the catalog but registering nothing.
	ConsumerOnly bool `json:"consumer_only"`
	// Repo is the package repository the files have been copied from, AltRepos are other repositories shipping the
	// same service (e.g. the community edition) that have been compared and not embedded.
	Repo     string       `json:"repo"`
	AltRepos []string     `json:"alt_repos"`
	Files    []SourceFile `json:"files"`

	fsys fs.FS
}

// index is the content of index.json.
type index struct {
	Services []Definition `json:"services"`
}

// Load returns the embedded definitions, sorted by name.
func Load() ([]Definition, error) {
	sub, err := fs.Sub(embedded, definitionsDir)
	if err != nil {
		return nil, err
	}

	return LoadFS(sub)
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

	_, err := d.Policy()
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
