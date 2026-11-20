// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package meshcmd contains the "service" commands of the agent and server CLIs: the node side of the central
// registration of the Carbonio services, see package mesh.
package meshcmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/mesh"
	"github.com/zextras/service-discover/pkg/spire"
)

const (
	// ExitRunScript is the exit status of "service pending-setup" asking the caller to run the pending setup script
	// itself, because this node or this script is not handled by the central registration.
	ExitRunScript = 10

	defaultWait = 30 * time.Second
	rootUID     = 0
)

var errNotRoot = errors.New("this command must be executed as root")

// Service groups the commands managing the credentials of the services of this node.
type Service struct {
	Sync         Sync         `cmd:"" help:"Obtain from SPIRE the credentials of the services installed on this node and register them in Consul"`  //nolint:lll
	PendingSetup PendingSetup `cmd:"" name:"pending-setup" help:"Run the mesh part of a pending setup script, as the central registration does it"` //nolint:lll
}

// New returns the Service commands writing on writer.
func New(writer io.Writer) Service {
	return Service{
		Sync: Sync{writer: writer, getuid: os.Getuid, spireMode: mesh.SPIREMode},
		PendingSetup: PendingSetup{
			writer: writer, getuid: os.Getuid, exit: os.Exit, run: runCommand, spireMode: mesh.SPIREMode,
		},
	}
}

// Sync is the "service sync" command.
type Sync struct {
	Service     []string      `name:"service" help:"Synchronize only this service (repeatable), by default all"`
	Renew       bool          `help:"Obtain a new token for every service, even if the current one is still valid"`
	NoRestart   bool          `name:"no-restart" help:"Only update the files: do not reload Consul nor restart any unit"`
	NoBlock     bool          `name:"no-block" help:"Do not wait for the restarted units to start"`
	Wait        time.Duration `default:"30s" help:"How long to wait for SPIRE to issue the identity of a service"`
	TrustDomain string        `name:"trust-domain" default:"carbonio" hidden:"" help:"SPIFFE trust domain"`

	writer    io.Writer   `kong:"-"`
	getuid    func() int  `kong:"-"`
	spireMode func() bool `kong:"-"`
}

// Run implements the command.
func (s *Sync) Run(_ *command.GlobalCommonFlags) error {
	if !s.spireMode() {
		_, err := fmt.Fprintln(s.writer, "This node does not use SPIRE: the services keep the token of the agent.")

		return err
	}

	if s.getuid() != rootUID {
		return errNotRoot
	}

	return s.syncServices(s.Service, false)
}

// syncServices synchronizes the services. With setup it also does what their package setup did when installed, and
// restarts them.
func (s *Sync) syncServices(services []string, setup bool) error {
	defs, err := mesh.Load()
	if err != nil {
		return err
	}

	env, err := mesh.NewEnv(s.TrustDomain)
	if err != nil {
		return err
	}

	return mesh.Sync(env, defs, mesh.SyncOptions{
		Services:     services,
		Renew:        s.Renew,
		NoRestart:    s.NoRestart,
		NoBlock:      s.NoBlock,
		Setup:        setup,
		ForceRestart: setup,
		Wait:         s.Wait,
		Out:          s.writer,
	})
}

// PendingSetup is the "service pending-setup" command, used by pending-setups instead of the script of a package on
// the nodes using SPIRE. The script obtains from the cluster, with the bootstrap token, what SPIRE and the central
// registration now provide (the ACL policy, the service protocol, the intentions and the token of the service): there
// is no bootstrap token on a node that joined through SPIRE.
//
// For the services whose script does nothing else the command is the whole setup. For the ones running other
// commands too, that only need the token of the service, those commands are run after the synchronization. In every
// other case the exit status is ExitRunScript and the caller runs the script.
type PendingSetup struct {
	Script      string        `arg:"" help:"Name of the pending setup script, e.g. 511-carbonio-files.sh"`
	Wait        time.Duration `default:"30s" help:"How long to wait for SPIRE to issue the identity of the service"`
	TrustDomain string        `name:"trust-domain" default:"carbonio" hidden:"" help:"SPIFFE trust domain"`

	writer io.Writer                               `kong:"-"`
	getuid func() int                              `kong:"-"`
	exit   func(int)                               `kong:"-"`
	run    func(name string, args ...string) error `kong:"-"`

	spireMode func() bool                               `kong:"-"`
	sync      func(services []string, setup bool) error `kong:"-"`
}

// Run implements the command.
func (p *PendingSetup) Run(_ *command.GlobalCommonFlags) error {
	if !p.spireMode() {
		p.exit(ExitRunScript)

		return nil
	}

	defs, err := mesh.Load()
	if err != nil {
		return err
	}

	def, found := mesh.PendingSetupService(defs, p.Script)
	if !found || def.PendingSetup != mesh.PendingSetupReplace {
		p.exit(ExitRunScript)

		return nil
	}

	if p.getuid() != rootUID {
		return errNotRoot
	}

	if p.sync == nil {
		synchronizer := Sync{TrustDomain: p.TrustDomain, Wait: p.Wait, writer: p.writer}
		p.sync = synchronizer.syncServices
	}

	// The script restarted the units, so the setup does too, and it sets up the services its package has besides.
	err = p.sync(append([]string{def.Name}, def.Companions...), true)
	if err != nil {
		return err
	}

	for _, post := range def.PostSetup {
		_, _ = fmt.Fprintf(p.writer, "%s: running %s\n", def.Name, post)

		fields := strings.Fields(post)
		if len(fields) == 0 {
			continue
		}

		err = p.run(fields[0], fields[1:]...)
		if err != nil {
			return fmt.Errorf("%s failed: %w", post, err)
		}
	}

	_, err = fmt.Fprintf(p.writer, "%s: the mesh registration and the token of %s are managed through SPIRE (%s)\n",
		p.Script, def.Name, spire.AuthMethodName)

	return err
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:noctx // command of a package, named by the embedded definitions
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
