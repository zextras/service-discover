// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package mesh implements the mesh command of the server CLI: the registration in Consul of the service mesh
// definitions of the Carbonio services, owned by service-discover.
package mesh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/mesh"
	"github.com/zextras/service-discover/pkg/spire"
)

const rootUID = 0

var (
	errNotRoot        = errors.New("this command must be executed as root")
	errUnknownService = errors.New("unknown service")
)

// Mesh groups the commands registering the services in the mesh.
type Mesh struct {
	Apply Apply `cmd:"" help:"Register in Consul the ACL policies, the roles, the SPIRE binding rules, the service protocols and the intentions of the Carbonio services"` //nolint:lll
}

// Apply is the "mesh apply" command. It is idempotent, and safe to run again after an upgrade: it brings the
// cluster to the definitions embedded in this version. Run it on a server, as root.
//
// On the server running the SPIRE server it also registers there the identities of the services of the server itself
// (the ones of the other nodes are registered by join-token): a server that was set up before SPIRE and got it with
// "spire enable" has none, and its services could not get a token from SPIRE.
type Apply struct {
	Service     []string `name:"service" help:"Register only this service (repeatable), by default all of them"`
	TrustDomain string   `name:"trust-domain" default:"carbonio" hidden:"" help:"SPIFFE trust domain"`

	bootstrap *command.BootstrapToken `kong:"-"`
	writer    io.Writer               `kong:"-"`
	getuid    func() int              `kong:"-"`

	apply       func(token, trustDomain string, services []string, out io.Writer) error `kong:"-"`
	runsSpire   func() bool                                                             `kong:"-"`
	hostname    func() (string, error)                                                  `kong:"-"`
	entries     mesh.EntryEnsurer                                                       `kong:"-"`
	definitions func() ([]mesh.Definition, error)                                       `kong:"-"`
}

// New returns the Mesh commands, asking the bootstrap token as the bootstrap-token command does, and writing on
// writer.
func New(bootstrap *command.BootstrapToken, writer io.Writer) Mesh {
	return Mesh{Apply: Apply{
		bootstrap:   bootstrap,
		writer:      writer,
		getuid:      os.Getuid,
		apply:       mesh.ApplyEmbedded,
		runsSpire:   runsSpireServer,
		hostname:    serverHostname,
		entries:     spire.NewServerCLI(exec.Command),
		definitions: mesh.Load,
	}}
}

// runsSpireServer tells if the SPIRE server of the cluster runs on this server.
func runsSpireServer() bool {
	info, err := os.Stat(filepath.Join(spire.ConfigDir, spire.ServerConfFile))

	return err == nil && info.Mode().IsRegular()
}

func serverHostname() (string, error) {
	localConfig, err := carbonio.LoadLocalConfig(config.LocalConfigPath)
	if err != nil {
		return "", err
	}

	return localConfig.Value(carbonio.LocalConfigServerHostname), nil
}

// Run implements the command.
func (a *Apply) Run(_ *command.GlobalCommonFlags) error {
	if a.getuid() != rootUID {
		return errNotRoot
	}

	token, err := a.bootstrap.Token()
	if err != nil {
		return err
	}

	err = a.apply(token, a.TrustDomain, a.Service, a.writer)
	if err != nil {
		return err
	}

	return a.registerLocalIdentities()
}

// registerLocalIdentities creates the SPIRE entries of the services of this server, when it runs the SPIRE server.
func (a *Apply) registerLocalIdentities() error {
	if !a.runsSpire() {
		return nil
	}

	host, err := a.hostname()
	if err != nil {
		return err
	}

	defs, err := a.definitions()
	if err != nil {
		return err
	}

	defs, err = selectServices(defs, a.Service)
	if err != nil {
		return err
	}

	created, err := mesh.EnsureEntries(a.entries, defs, a.TrustDomain, host)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(a.writer, "SPIRE: %d identities of the services of %s registered (%d definitions)\n",
		created, host, len(defs))

	return err
}

func selectServices(defs []mesh.Definition, names []string) ([]mesh.Definition, error) {
	if len(names) == 0 {
		return defs, nil
	}

	selected := make([]mesh.Definition, 0, len(names))

	for _, name := range names {
		def, found := mesh.Find(defs, name)
		if !found {
			return nil, fmt.Errorf("%w: %s", errUnknownService, name)
		}

		selected = append(selected, *def)
	}

	return selected, nil
}
