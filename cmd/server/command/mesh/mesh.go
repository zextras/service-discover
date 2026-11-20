// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package mesh implements the mesh command of the server CLI: the registration in Consul of the service mesh
// definitions of the Carbonio services, owned by service-discover.
package mesh

import (
	"errors"
	"io"
	"os"

	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/mesh"
)

const rootUID = 0

var errNotRoot = errors.New("this command must be executed as root")

// Mesh groups the commands registering the services in the mesh.
type Mesh struct {
	Apply Apply `cmd:"" help:"Register in Consul the ACL policies, the roles, the SPIRE binding rules, the service protocols and the intentions of the Carbonio services"` //nolint:lll
}

// Apply is the "mesh apply" command. It is idempotent, and safe to run again after an upgrade: it brings the
// cluster to the definitions embedded in this version. Run it on a server, as root.
type Apply struct {
	Service     []string `name:"service" help:"Register only this service (repeatable), by default all of them"`
	TrustDomain string   `name:"trust-domain" default:"carbonio" hidden:"" help:"SPIFFE trust domain"`

	bootstrap *command.BootstrapToken `kong:"-"`
	writer    io.Writer               `kong:"-"`
	getuid    func() int              `kong:"-"`
}

// New returns the Mesh commands, asking the bootstrap token as the bootstrap-token command does, and writing on
// writer.
func New(bootstrap *command.BootstrapToken, writer io.Writer) Mesh {
	return Mesh{Apply: Apply{bootstrap: bootstrap, writer: writer, getuid: os.Getuid}}
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

	return mesh.ApplyEmbedded(token, a.TrustDomain, a.Service, a.writer)
}
