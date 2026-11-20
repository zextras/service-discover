// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/consul/api/v2"
)

// Command is a process to run.
type Command struct {
	Name string
	Args []string
	// Env is added to the environment of the process.
	Env []string
	// Stdin is the input of the process.
	Stdin string
	// Out receives the output (stdout and stderr) of the process, that is returned when nil.
	Out io.Writer
}

// Instance is an instance of a Consul service.
type Instance struct {
	Address string
	Meta    map[string]string
}

// Consul is the part of the Consul API the setup of a service uses, always with the token of the service.
type Consul interface {
	KV
	// Instances returns the instances of the service, only the ones passing their checks when passing is set.
	Instances(service string, passing bool) ([]Instance, error)
	SetConfigEntry(entry api.ConfigEntry) error
}

var (
	errNoValue     = errors.New("no value")
	errUnreadable  = errors.New("unreadable")
	errCertificate = errors.New("TLS certificate")
)

// step is a part of the setup of a service implemented here, because the script of the package that does it needs the
// bootstrap token or does not work with the token of the service.
type step struct {
	// prepare runs before the definitions of the service are rendered, finish once the service is registered.
	prepare func(*setupContext) error
	finish  func(*setupContext) error
}

var steps = map[string]step{
	"docs-editor":  {prepare: prepareDocsEditor, finish: finishDocsEditor},
	"turn-server":  {prepare: prepareTurnServer},
	"alertmanager": {prepare: prepareAlertmanager},
}

type setupContext struct {
	env   *Env
	def   *Definition
	token string
	log   func(string, ...any)
	out   io.Writer
}

// prepareService runs what the setup of a service does before its definitions are rendered: the migrations of its
// configuration, its own step and the commands generating its configuration.
func prepareService(ctx *setupContext) error {
	err := Migrate(ctx.env, ctx.def, ctx.token, ctx.log)
	if err != nil {
		return fmt.Errorf("migration: %w", err)
	}

	if ctx.def.Setup != "" && steps[ctx.def.Setup].prepare != nil {
		err = steps[ctx.def.Setup].prepare(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", ctx.def.Setup, err)
		}
	}

	return runCommands(ctx, ctx.def.Configure)
}

// finishService runs what the setup of a service does once the service is registered: the routing.
func finishService(ctx *setupContext, wait time.Duration) error {
	if ctx.def.Setup != "" && steps[ctx.def.Setup].finish != nil {
		err := steps[ctx.def.Setup].finish(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", ctx.def.Setup, err)
		}
	}

	if len(ctx.def.Routing) == 0 {
		return nil
	}

	waitLocalInstance(ctx, wait)

	return runCommands(ctx, ctx.def.Routing)
}

// waitLocalInstance waits for the service to pass its checks on this node: the routing is made of the instances that
// are healthy. It gives up silently, the routing is then missing the local instance until the next setup.
func waitLocalInstance(ctx *setupContext, wait time.Duration) {
	consul, err := ctx.env.Consul(ctx.token)
	if err != nil {
		return
	}

	deadline := ctx.env.Now().Add(wait)

	for {
		instances, err := consul.Instances(ctx.def.Name, true)
		if err == nil && hasLocal(ctx.env, instances) {
			return
		}

		if !ctx.env.Now().Before(deadline) {
			ctx.log("%s: not healthy on this node yet, the routing goes without it", ctx.def.Name)

			return
		}

		ctx.env.Sleep(fetchInterval)
	}
}

func hasLocal(env *Env, instances []Instance) bool {
	local := localAddresses(env)

	for _, instance := range instances {
		if local[instance.Address] {
			return true
		}
	}

	return false
}

// localAddresses returns the addresses of this node.
func localAddresses(env *Env) map[string]bool {
	addresses := map[string]bool{}

	for field := range strings.FieldsSeq(hostnameOutput(env, "-I")) {
		addresses[field] = true
	}

	if len(addresses) > 0 {
		return addresses
	}

	list, err := net.InterfaceAddrs()
	if err != nil {
		return addresses
	}

	for _, address := range list {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr == nil && !ip.IsLoopback() {
			addresses[ip.String()] = true
		}
	}

	return addresses
}

// localIP is the first address of the node, the one the package scripts use as LOCAL_IP.
func localIP(env *Env) string {
	fields := strings.Fields(hostnameOutput(env, "-I"))
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

// hostnameOutput returns the trimmed output of hostname, empty when it fails.
func hostnameOutput(env *Env, flag string) string {
	out, err := env.Exec(&Command{Name: "hostname", Args: []string{flag}})
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// runCommands runs commands of the package with the token of the service, as they run in its setup.
func runCommands(ctx *setupContext, commands []string) error {
	for _, line := range commands {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		ctx.log("%s: running %s", ctx.def.Name, line)

		_, err := ctx.env.Exec(&Command{
			Name: fields[0],
			Args: fields[1:],
			Env: []string{
				"CONSUL_HTTP_TOKEN=" + ctx.token,
				"SETUP_CONSUL_TOKEN=" + ctx.token,
				"LOCAL_IP=" + localIP(ctx.env),
			},
			Out: ctx.out,
		})
		if err != nil {
			return fmt.Errorf("%s failed: %w", line, err)
		}
	}

	return nil
}

// ensureOwnedFile writes a file for the account, replacing it only when the content differs.
func ensureOwnedFile(env *Env, path string, content []byte, mode os.FileMode, account Account) error {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, content) {
		return nil
	}

	return writeFile(env, path, content, mode, account)
}

func joinCarbonio(env *Env, elem ...string) string {
	return filepath.Join(append([]string{env.CarbonioDir}, elem...)...)
}
