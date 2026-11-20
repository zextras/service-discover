// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

var (
	errBadSidecar    = errors.New("a sidecar unit must be named *-sidecar.service")
	errConsumerHCL   = errors.New("a consumer only service registers nothing, it has no hcl file")
	errGeneratedHCL  = errors.New("a generated service lists the hcl files its setup generates")
	errNoCompanion   = errors.New("unknown companion")
	errEmptyCommand  = errors.New("empty command")
	errNoRepo        = errors.New("the repository the files come from is required")
	errNoSidecarRead = errors.New("the extra policy must let the sidecar read the services: it has no upstream without it")
	errWriteServices = errors.New("the access to the services of the extra policy must be read only")
	errUnregistered  = errors.New("a service registering nothing has no rule on the services in its extra policy")
	errNoName        = errors.New("a config entry has no name")
)

// ValidateDir reads the definitions of dir the way Load does, without the checks of the installed package, and checks
// what loading does not: the rules the definitions have to follow to be applied to a cluster, across their files. It
// returns the definitions, and an error listing every problem it found, one per line. It needs no Consul.
func ValidateDir(dir string) ([]Definition, error) {
	defs, err := LoadDir(dir)
	if err != nil {
		return nil, err
	}

	var problems []error

	for i := range defs {
		for _, err := range defs[i].check(defs) {
			problems = append(problems, fmt.Errorf("%s: %w", defs[i].Name, err))
		}
	}

	return defs, errors.Join(problems...)
}

// check returns the problems of a definition against the others.
func (d *Definition) check(defs []Definition) []error {
	var problems []error

	fail := func(err error) { problems = append(problems, err) }

	if d.Repo == "" {
		fail(errNoRepo)
	}

	entries, err := d.ConfigEntries()
	if err != nil {
		fail(err)
	}

	for _, entry := range entries {
		if entry.GetName() == "" {
			fail(errNoName)
		}
	}

	for _, unit := range d.Sidecars {
		if !strings.HasSuffix(unit, "-sidecar.service") {
			fail(fmt.Errorf("%w: %s", errBadSidecar, unit))
		}
	}

	if d.ConsumerOnly && len(d.HCL) > 0 {
		fail(errConsumerHCL)
	}

	if d.Generated && len(d.HCL) == 0 {
		fail(errGeneratedHCL)
	}

	for _, companion := range d.Companions {
		if _, found := Find(defs, companion); !found {
			fail(fmt.Errorf("%w: %s", errNoCompanion, companion))
		}
	}

	for _, command := range slices.Concat(d.Configure, d.Routing, d.PostSetup) {
		if len(strings.Fields(command)) == 0 {
			fail(errEmptyCommand)
		}
	}

	return append(problems, d.checkExtraPolicy()...)
}

// checkExtraPolicy checks the rules of the policy linked to the role of the service: a service registering something
// reads the services, for the sidecar to resolve its upstreams, and never writes them but its own.
func (d *Definition) checkExtraPolicy() []error {
	raw, err := d.ExtraPolicy()
	if err != nil {
		return []error{err}
	}

	if len(d.HCL) == 0 {
		if strings.Contains(raw, "service_prefix") {
			return []error{errUnregistered}
		}

		return nil
	}

	var rules Rules

	err = json.Unmarshal([]byte(raw), &rules)
	if err != nil {
		return []error{err}
	}

	var problems []error

	if !readsAllServices(rules) {
		problems = append(problems, errNoSidecarRead)
	}

	for _, entry := range rules["service_prefix"] {
		for prefix, rule := range entry {
			if rule.Policy != policyRead {
				problems = append(problems, fmt.Errorf("%w: %q is %s", errWriteServices, prefix, rule.Policy))
			}
		}
	}

	return problems
}
