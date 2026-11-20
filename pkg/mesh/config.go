// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zextras/service-discover/pkg/spire"
)

// SPIREMode reports whether this node uses SPIRE: the SPIRE agent has been configured, by joining the cluster with a
// join bundle or by setting up a server. Only these nodes get the credentials of their services from SPIRE; the
// others keep registering their services with the token of the agent.
func SPIREMode() bool {
	return isFile(filepath.Join(spire.ConfigDir, spire.AgentConfFile))
}

// ConsulConfigArgs prepares the configuration of Consul on a node using SPIRE and returns the arguments to load it.
//
// Consul loads one directory, RenderedDir, instead of the one of the packages. It holds the service definitions of the
// packages with the token of their service injected, and a verbatim copy of everything else (the configuration of the
// agent, the definitions of services unknown to service-discover). The services without a token yet are not
// registered, they are as soon as the synchronization has issued it. A directory, unlike a list of files, is read
// again by Consul when it reloads: whatever a package installs later reaches the running agent with a reload. It has
// to run as root, before dropping the privileges: the tokens are readable by the service users only.
//
// The arguments are usable whenever they are not nil: the error may then only report the services that could not be
// rendered, that are left out, and Consul can start anyway.
func ConsulConfigArgs(env *Env, defs []Definition) ([]string, error) {
	renderErr := renderOffline(env, defs)

	if !isDir(env.RenderedDir) {
		return nil, errors.Join(errNoRenderedDir, renderErr)
	}

	return []string{"-config-dir", env.RenderedDir}, renderErr
}

var errNoRenderedDir = errors.New("the directory of the rendered service definitions is not available")

func isConfigFile(name string) bool {
	ext := filepath.Ext(name)

	return !strings.HasPrefix(name, ".") && (ext == ".json" || ext == ".hcl")
}

// renderOffline renders the definitions of the installed services that already have a token, without talking to
// Consul, and removes the stale ones.
func renderOffline(env *Env, defs []Definition) error {
	consul, err := env.LookupUser(ConsulUser)
	if err != nil {
		return fmt.Errorf("unable to find the Consul user: %w", err)
	}

	err = os.MkdirAll(env.RenderedDir, renderedDirMode)
	if err != nil {
		return err
	}

	err = env.Chown(env.RenderedDir, consul.UID, consul.GID)
	if err != nil {
		return err
	}

	discard := func(string, ...any) {}
	failures := []error{}

	installed := env.Installed(defs)

	for i := range installed {
		def := &installed[i]

		token := readToken(env.tokenFile(def))
		if token == "" || def.ConsumerOnly {
			continue
		}

		_, renderErr := renderDefinitions(env, def, token, discard)
		if renderErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", def.Name, renderErr))
		}
	}

	_, err = mirrorPassThrough(env, defs, discard)
	failures = append(failures, err)

	_, err = removeStale(env, defs, discard)

	return errors.Join(append(failures, err)...)
}

// withToken returns the definitions that can be rendered, i.e. whose token exists: the rendered definition of a
// service losing its token (or its package) has to go.
func withToken(env *Env, defs []Definition) []Definition {
	installed := env.Installed(defs)

	return slices.DeleteFunc(installed, func(def Definition) bool {
		return def.ConsumerOnly || readToken(env.tokenFile(&def)) == ""
	})
}
