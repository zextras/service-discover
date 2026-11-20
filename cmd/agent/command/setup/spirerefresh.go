// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/spire"
)

// Spire groups the commands managing the SPIRE agent of this node.
type Spire struct {
	Servers Servers `cmd:"" help:"Manage the SPIRE servers this agent connects to"`
}

// Servers groups the commands about the SPIRE servers of this node.
type Servers struct {
	Refresh Refresh `cmd:"" help:"Update the SPIRE servers this agent connects to with the Consul servers of the cluster. Needed when Consul servers running SPIRE are added or removed after the agent joined"` //nolint:lll
}

// NewSpire returns the spire command with the real paths of the installation.
func NewSpire() Spire {
	return Spire{Servers: Servers{Refresh: Refresh{setup: New()}}}
}

// Refresh rewrites the block of the hosts file that gives one name to all the SPIRE servers.
type Refresh struct {
	setup Setup `kong:"-"`
}

type refreshOutput struct {
	Steps []string `json:"steps"`
}

func (o *refreshOutput) PlainRender() (string, error) { return strings.Join(o.Steps, "\n") + "\n", nil }

func (o *refreshOutput) JSONRender() (string, error) { return formatter.DefaultJSONRender(o) }

func (r *Refresh) Run(commonFlags *command.GlobalCommonFlags) error {
	deps := realDependencies{}

	if deps.GetuidSyscall() != rootUID {
		return errors.New("this command must be executed as root")
	}

	out, err := r.setup.refreshSpireServers(deps)
	if err != nil {
		return err
	}

	render, err := formatter.Render(out, commonFlags.Format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(os.Stdout, render)

	return err
}

var (
	serverAddressRegexp = regexp.MustCompile(`(?m)^\s*server_address\s*=\s*"([^"]*)"`)
	serverPortRegexp    = regexp.MustCompile(`(?m)^\s*server_port\s*=\s*"?(\d+)"?`)
)

// refreshSpireServers points the name the SPIRE agent connects to at the Consul servers that answer on the SPIRE
// port, as listed by the local Consul agent. Only the lines of the block managed by service-discover in the hosts
// file are written. The SPIRE agent resolves the name again by itself when a connection is lost, no restart is
// needed. Without a block (the agent connects to a single server) there is nothing to do.
func (s *Setup) refreshSpireServers(deps businessDependencies) (formatter.Formatter, error) {
	content, err := os.ReadFile(filepath.Join(s.SpireConfigDir, spire.AgentConfFile)) // #nosec
	if err != nil {
		return nil, errors.WithMessage(err, "unable to read the configuration of the SPIRE agent: was this node joined?")
	}

	address := serverAddressRegexp.FindSubmatch(content)
	port := serverPortRegexp.FindSubmatch(content)

	if address == nil || port == nil {
		return nil, errors.New("unable to read the SPIRE server address from the SPIRE agent configuration")
	}

	if string(address[1]) != spireServersAlias {
		return &refreshOutput{Steps: []string{
			"This agent connects to " + string(address[1]) + " only, there is nothing to refresh",
		}}, nil
	}

	spirePort, err := strconv.Atoi(string(port[1]))
	if err != nil {
		return nil, err
	}

	peers, err := consulStatusPeers()
	if err != nil {
		return nil, errors.WithMessage(err, "unable to list the Consul servers from the local Consul agent")
	}

	hosts := make([]string, 0, len(peers))

	for _, peer := range peers {
		host, _, err := net.SplitHostPort(peer)
		if err != nil {
			host = peer
		}

		hosts = append(hosts, host)
	}

	reachable := reachableSpireServers(deps, hosts, spirePort)
	if len(reachable) == 0 {
		return nil, errors.New("no Consul server answers on the SPIRE port: the hosts file was left as it is")
	}

	err = writeHostsBlock(s.HostsFile, spireServersAlias, reachable)
	if err != nil {
		return nil, errors.WithMessagef(err, "unable to update %s", s.HostsFile)
	}

	return &refreshOutput{Steps: []string{
		spireServersAlias + " now resolves to " + strings.Join(reachable, ", "),
	}}, nil
}
