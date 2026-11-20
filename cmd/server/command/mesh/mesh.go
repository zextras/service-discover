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
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/consul/api/v2"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/legacytoken"
	"github.com/zextras/service-discover/pkg/mesh"
	"github.com/zextras/service-discover/pkg/spire"
)

const rootUID = 0

// Locations read by the commands, variables for the tests.
var (
	spireServerConf = filepath.Join(spire.ConfigDir, spire.ServerConfFile)
	localConfigPath = config.LocalConfigPath
)

var (
	errNotRoot        = errors.New("this command must be executed as root")
	errUnknownService = errors.New("unknown service")
	errNoLeader       = errors.New("the cluster has no leader")

	errUnattendedRevoke = errors.New("--revoke-legacy needs the bootstrap token typed in: it cannot be --unattended")
)

// Mesh groups the commands registering the services in the mesh.
type Mesh struct {
	Apply Apply `cmd:"" help:"Register in Consul the ACL policies, the roles, the SPIRE binding rules, the service protocols and the intentions of the Carbonio services"` //nolint:lll
}

// revokeFunc deletes the legacy tokens of the services the selector accepts, and returns the ones it deleted and the
// ones it kept.
type revokeFunc func(token string, selector legacytoken.Selector) ([]legacytoken.Token, []legacytoken.Kept, error)

// Apply is the "mesh apply" command. It is idempotent, and safe to run again after an upgrade: it brings the
// cluster to the definitions of the installed service-discover-specs package. Run it on a server, as root.
//
// On the server running the SPIRE server it also registers there the identities of the services of the server itself
// and of the other nodes the SPIRE server knows (join-token registered the ones of the definitions of the time a node
// joined): a server that was set up before SPIRE and got it with "spire enable" has none, a service added to the
// definitions later has none for the existing nodes, and without them the services could not get a token from SPIRE.
//
// With --unattended it is the run of the systemd unit that follows the installation of the definitions of the services
// (the service-discover-specs package): see runUnattended.
//
// With --revoke-legacy it then deletes the legacy per-service tokens of this server, the ones the package scripts
// created before SPIRE. Run it after "service sync" has put the login tokens in place: only the services whose token
// file holds a valid login token, and whose identity SPIRE has registered for this server, lose their legacy token.
type Apply struct {
	Service      []string      `name:"service" help:"Register only this service (repeatable), by default all of them"`
	TrustDomain  string        `name:"trust-domain" default:"carbonio" hidden:"" help:"SPIFFE trust domain"`
	RevokeLegacy bool          `name:"revoke-legacy" help:"Also delete the legacy ACL tokens of the services of this server, those of the services that already run with a SPIRE login token (run 'service sync' first)"`                                                                  //nolint:lll
	Unattended   bool          `name:"unattended" help:"For the systemd unit watching the service-discover-specs package: ask nothing, take the bootstrap token from the password the setup stored, act only on the Raft leader, and skip with a message when the token is not available"` //nolint:lll
	Wait         time.Duration `name:"wait" default:"30s" help:"With --unattended, how long to wait for the cluster to have a leader and for the definitions of the package to be complete"`                                                                                               //nolint:lll
	Settle       time.Duration `name:"settle" help:"With --unattended, wait this long first, and run again if the definitions changed meanwhile: the run starts at the first file a package unpacks"`                                                                                      //nolint:lll

	bootstrap *command.BootstrapToken `kong:"-"`
	writer    io.Writer               `kong:"-"`
	getuid    func() int              `kong:"-"`

	apply       func(token, trustDomain string, services []string, out io.Writer) error `kong:"-"`
	runsSpire   func() bool                                                             `kong:"-"`
	hostname    func() (string, error)                                                  `kong:"-"`
	entries     mesh.EntryEnsurer                                                       `kong:"-"`
	definitions func() ([]mesh.Definition, error)                                       `kong:"-"`

	manifest     func() (*mesh.Manifest, error) `kong:"-"`
	nodes        func() ([]string, error)       `kong:"-"`
	leader       func() (leaderInfo, error)     `kong:"-"`
	sleep        func(time.Duration)            `kong:"-"`
	passwordFile string                         `kong:"-"`

	hasEntry     func(parentID, spiffeID string) (bool, error)                     `kong:"-"`
	loginInPlace func(trustDomain string) (func(def *mesh.Definition) bool, error) `kong:"-"`
	revoke       revokeFunc                                                        `kong:"-"`
}

// New returns the Mesh commands, asking the bootstrap token as the bootstrap-token command does, and writing on
// writer.
func New(bootstrap *command.BootstrapToken, writer io.Writer) Mesh {
	return Mesh{Apply: Apply{
		bootstrap:    bootstrap,
		writer:       writer,
		getuid:       os.Getuid,
		apply:        mesh.ApplySpecs,
		runsSpire:    runsSpireServer,
		hostname:     serverHostname,
		entries:      spire.NewServerCLI(exec.Command),
		definitions:  mesh.Load,
		manifest:     installedManifest,
		nodes:        spire.NewServerCLI(exec.Command).NodeHosts,
		leader:       raftLeader,
		sleep:        time.Sleep,
		passwordFile: filepath.Join(config.ConsulHome, "password"),

		hasEntry:     spire.NewServerCLI(exec.Command).HasEntry,
		loginInPlace: loginTokens,
		revoke:       revokeWithToken,
	}}
}

// loginTokens returns the check telling if the service of a definition runs with a valid SPIRE login token on this
// node: the token file holds one.
func loginTokens(trustDomain string) (func(def *mesh.Definition) bool, error) {
	env, err := mesh.NewEnv(trustDomain)
	if err != nil {
		return nil, err
	}

	return env.HasLoginToken, nil
}

func revokeWithToken(token string, selector legacytoken.Selector) ([]legacytoken.Token, []legacytoken.Kept, error) {
	cfg := api.DefaultConfig()
	cfg.Token = token

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, nil, err
	}

	return legacytoken.RevokeLegacyServiceTokens(legacytoken.NewClientACL(client), selector)
}

// runsSpireServer tells if the SPIRE server of the cluster runs on this server.
func runsSpireServer() bool {
	info, err := os.Stat(spireServerConf)

	return err == nil && info.Mode().IsRegular()
}

func serverHostname() (string, error) {
	localConfig, err := carbonio.LoadLocalConfig(localConfigPath)
	if err != nil {
		return "", err
	}

	return localConfig.Value(carbonio.LocalConfigServerHostname), nil
}

// leaderInfo is what a server knows of the Raft leader of the cluster.
type leaderInfo struct {
	// Address is the address of the leader, empty while the cluster has none.
	Address string
	// Local tells that the leader is this server.
	Local bool
}

// installedManifest returns the manifest of the definitions this server reads.
func installedManifest() (*mesh.Manifest, error) {
	dir, _ := mesh.SpecsDir()

	return mesh.ReadManifest(dir)
}

// raftLeader asks the local Consul agent which server is the leader. The question needs no ACL token. The leader is
// the local server when its address is one of the addresses of this machine.
func raftLeader() (leaderInfo, error) {
	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return leaderInfo{}, err
	}

	address, err := client.Status().Leader()
	if err != nil {
		return leaderInfo{}, fmt.Errorf("unable to ask Consul for its leader: %w", err)
	}

	if address == "" {
		return leaderInfo{}, nil
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}

	leader := net.ParseIP(host)

	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return leaderInfo{}, fmt.Errorf("unable to list the addresses of this server: %w", err)
	}

	for _, local := range addresses {
		ipNet, ok := local.(*net.IPNet)
		if ok && leader != nil && ipNet.IP.Equal(leader) {
			return leaderInfo{Address: address, Local: true}, nil
		}
	}

	return leaderInfo{Address: address}, nil
}

// Run implements the command.
func (a *Apply) Run(_ *command.GlobalCommonFlags) error {
	if a.getuid() != rootUID {
		return errNotRoot
	}

	if a.Unattended {
		return a.runUnattended()
	}

	token, err := a.bootstrap.Token()
	if err != nil {
		return err
	}

	err = a.apply(token, a.TrustDomain, a.Service, a.writer)
	if err != nil {
		return err
	}

	err = a.registerLocalIdentities()
	if err != nil {
		return err
	}

	if a.RevokeLegacy {
		return a.revokeLegacyTokens(token)
	}

	return nil
}

// runUnattended is the run of the systemd unit watching the definitions package: the package of a server changed them,
// and the cluster has to follow. Every server of the cluster has the unit and gets the same package, so it is the
// leader that registers them in Consul, once; the others have nothing to do there. It needs the bootstrap token, and
// nobody to ask for it: it comes from the password the setup of the server stored for root. A server without it logs
// that it did nothing, and "mesh apply" has to be run by hand.
//
// The SPIRE entries are created by the server running the SPIRE server, whether it leads or not: they have to exist
// for every node before it can get the credentials of a service it has installed.
func (a *Apply) runUnattended() error {
	if a.RevokeLegacy {
		return errUnattendedRevoke
	}

	// The unit starts at the first file the package unpacks: wait for the rest of the files, and run again if the
	// package changed the definitions while the run was going on.
	for {
		a.sleep(a.Settle)

		before, err := a.waitSpecs()
		if err != nil {
			return err
		}

		err = a.applyAsLeader()
		if err != nil {
			return err
		}

		after, err := a.manifest()
		if err != nil || after.TreeSHA256 == before.TreeSHA256 {
			return err
		}
	}
}

// waitSpecs waits for the definitions to be the ones of the package, complete and matching its manifest, the time of
// --wait: while the package is being installed they are not.
func (a *Apply) waitSpecs() (*mesh.Manifest, error) {
	const interval = time.Second

	for waited := time.Duration(0); ; waited += interval {
		_, err := a.definitions()
		if err == nil {
			return a.manifest()
		}

		if waited >= a.Wait {
			return nil, err
		}

		a.sleep(interval)
	}
}

func (a *Apply) applyAsLeader() error {
	leader, err := a.waitLeader()
	if err != nil {
		return err
	}

	if !leader.Local {
		_, err = fmt.Fprintf(a.writer, "This server does not lead the cluster (the leader is %s): it applies nothing\n",
			leader.Address)
		if err != nil {
			return err
		}

		return a.registerLocalIdentities()
	}

	token, err := a.bootstrap.StoredToken(a.passwordFile)

	switch {
	case errors.Is(err, command.ErrNoStoredPassword):
		_, err = fmt.Fprintf(a.writer, "This server leads the cluster but has no stored cluster password (%s): "+
			"the service definitions are not applied, run 'service-discover mesh apply' to apply them\n", a.passwordFile)

		return err
	case err != nil:
		return fmt.Errorf("unable to get the bootstrap token: %w", err)
	}

	err = a.apply(token, a.TrustDomain, a.Service, a.writer)
	if err != nil {
		return err
	}

	return a.registerLocalIdentities()
}

// waitLeader waits for the cluster to have a leader, the time of --wait: the definitions may change while a server
// is restarting after the upgrade that installed them.
func (a *Apply) waitLeader() (leaderInfo, error) {
	const interval = 2 * time.Second

	for waited := time.Duration(0); ; waited += interval {
		leader, err := a.leader()
		if err == nil && leader.Address != "" {
			return leader, nil
		}

		if waited >= a.Wait {
			if err == nil {
				err = errNoLeader
			}

			return leaderInfo{}, err
		}

		a.sleep(interval)
	}
}

// revokeLegacyTokens deletes the legacy tokens of the services of this server that run with a login token: the
// legacy token of a service whose login token is not in place yet is the only one it has.
func (a *Apply) revokeLegacyTokens(token string) error {
	host, err := a.hostname()
	if err != nil {
		return err
	}

	defs, err := a.definitions()
	if err != nil {
		return err
	}

	hasLogin, err := a.loginInPlace(a.TrustDomain)
	if err != nil {
		return err
	}

	ready := func(service string) (bool, error) {
		def, found := mesh.Find(defs, service)
		if !found || !hasLogin(def) {
			return false, nil
		}

		return a.hasEntry(spire.NodeID(host), spire.ServiceID(service))
	}

	revoked, kept, err := a.revoke(token, legacytoken.NewSelector(command.Server, host, ready))

	for _, old := range revoked {
		_, _ = fmt.Fprintf(a.writer, "Legacy ACL token %s of %s deleted\n", old.AccessorID, old.Service)
	}

	for _, keep := range kept {
		_, _ = fmt.Fprintf(a.writer, "Legacy ACL token %s of %s kept: %s\n",
			keep.Token.AccessorID, keep.Token.Service, keep.Reason)
	}

	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(a.writer, "Legacy ACL tokens: %d deleted, %d kept\n", len(revoked), len(kept))

	return err
}

// registerLocalIdentities creates the SPIRE entries of the services of this server and of the other nodes, when this
// server runs the SPIRE server. A service the definitions gained after a node joined has no entry for it: the join
// token created them, and only for the definitions that existed then.
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

	hosts, err := a.hosts(host)
	if err != nil {
		return err
	}

	created := 0

	for _, node := range hosts {
		count, ensureErr := mesh.EnsureEntries(a.entries, defs, a.TrustDomain, node)
		if ensureErr != nil {
			return ensureErr
		}

		created += count
	}

	_, err = fmt.Fprintf(a.writer, "SPIRE: %d identities of the services of %s registered (%d definitions)\n",
		created, strings.Join(hosts, ", "), len(defs))

	return err
}

// hosts returns this server and the other nodes the SPIRE server knows, this one first.
func (a *Apply) hosts(local string) ([]string, error) {
	hosts := []string{local}

	if a.nodes == nil {
		return hosts, nil
	}

	nodes, err := a.nodes()
	if err != nil {
		return nil, err
	}

	for _, node := range nodes {
		if node != local {
			hosts = append(hosts, node)
		}
	}

	return hosts, nil
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
