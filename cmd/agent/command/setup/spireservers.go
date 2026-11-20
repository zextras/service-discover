// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	// spireServersAlias is the name the SPIRE agent connects to when more than one SPIRE server is available. The
	// agent resolves it to every address and balances the connections among them, moving to another server when one
	// is lost. Which server it talks to does not matter: the agent authenticates it by its SPIFFE ID, not by name.
	spireServersAlias = "spire-server.carbonio"
	hostsBegin        = "# BEGIN service-discover spire servers"
	hostsEnd          = "# END service-discover spire servers"
	spireDialTimeout  = 3 * time.Second
	hostsFileMode     = 0644
)

// dialSpireServer checks that something accepts connections on address. It is a variable for the tests.
var dialSpireServer = func(address string) error {
	conn, err := net.DialTimeout("tcp", address, spireDialTimeout)
	if err != nil {
		return err
	}

	return conn.Close()
}

// spireServerAddress returns the server_address of the SPIRE agent. The SPIRE servers of a high availability cluster
// run on the Consul servers, so the candidates are the host in the join bundle and the Consul servers it lists: those
// answering on the SPIRE port are the ones to use. With a single one, its address is used: the SPIRE agent dials a
// name through the gRPC DNS resolver, which looks up SRV records of the name in DNS even when /etc/hosts has it, and a
// DNS server not answering for it delays every connection of the agent by about 30 seconds. With more, they are given
// a common name in /etc/hosts, so that the agent survives the loss of any of them. If none answers, the host of the
// bundle is used as it is.
func (s *Setup) spireServerAddress(
	deps businessDependencies, spireHost string, spirePort int, consulServers []string,
) (string, error) {
	candidates := make([]string, 0, 1+len(consulServers))
	candidates = append(candidates, spireHost)

	for _, server := range consulServers {
		host, _, err := net.SplitHostPort(server)
		if err != nil {
			host = server
		}

		candidates = append(candidates, host)
	}

	reachable := reachableSpireServers(deps, candidates, spirePort)

	switch len(reachable) {
	case 0:
		return spireHost, nil
	case 1:
		return reachable[0], nil
	}

	err := writeHostsBlock(s.HostsFile, spireServersAlias, reachable)
	if err != nil {
		return "", errors.WithMessagef(err, "unable to map %s to the SPIRE servers in %s", spireServersAlias, s.HostsFile)
	}

	return spireServersAlias, nil
}

// reachableSpireServers resolves the hosts and keeps the addresses accepting connections on the SPIRE port, in the
// order of the hosts and without duplicates.
func reachableSpireServers(deps businessDependencies, hosts []string, spirePort int) []string {
	reachable := []string{}
	seen := map[string]bool{}

	for _, host := range hosts {
		for _, address := range resolveHost(deps, host) {
			if seen[address] {
				continue
			}

			seen[address] = true

			if dialSpireServer(net.JoinHostPort(address, strconv.Itoa(spirePort))) == nil {
				reachable = append(reachable, address)
			}
		}
	}

	return reachable
}

func resolveHost(deps businessDependencies, host string) []string {
	if ip := net.ParseIP(host); ip != nil {
		return []string{ip.String()}
	}

	ips, err := deps.LookupIP(host)
	if err != nil {
		return nil
	}

	resolved := make([]string, 0, len(ips))
	for _, ip := range ips {
		resolved = append(resolved, ip.String())
	}

	return resolved
}

// stripHostsBlock removes the block managed by service-discover from the content of a hosts file.
func stripHostsBlock(content string) string {
	var kept []string

	skipping := false

	for line := range strings.SplitSeq(content, "\n") {
		switch {
		case strings.TrimSpace(line) == hostsBegin:
			skipping = true
		case skipping && strings.TrimSpace(line) == hostsEnd:
			skipping = false
		case !skipping:
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, "\n")
}

// writeHostsBlock maps alias to every address of ips in a block of the hosts file, replacing the previous one.
func writeHostsBlock(path, alias string, ips []string) error {
	content, err := os.ReadFile(path) // #nosec
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	rest := strings.TrimRight(stripHostsBlock(string(content)), "\n")
	if rest != "" {
		rest += "\n"
	}

	var block strings.Builder

	block.WriteString(hostsBegin + "\n")

	for _, address := range ips {
		block.WriteString(address + " " + alias + "\n")
	}

	block.WriteString(hostsEnd + "\n")

	if string(content) == rest+block.String() {
		return nil
	}

	return replaceFile(path, []byte(rest+block.String()))
}

// removeHostsBlock drops the block managed by service-discover from the hosts file, if there is one.
func removeHostsBlock(path string) error {
	content, err := os.ReadFile(path) // #nosec
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	if !strings.Contains(string(content), hostsBegin) {
		return nil
	}

	return replaceFile(path, []byte(strings.TrimRight(stripHostsBlock(string(content)), "\n")+"\n"))
}

// replaceFile atomically replaces the file, keeping its mode: a reader never sees half of a hosts file.
func replaceFile(path string, content []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		target = path
	}

	mode := os.FileMode(hostsFileMode)

	info, err := os.Stat(target)
	if err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".hosts-")
	if err != nil {
		return err
	}

	defer os.Remove(tmp.Name())

	_, err = tmp.Write(content)
	if err != nil {
		tmp.Close()

		return err
	}

	err = tmp.Chmod(mode)
	if err != nil {
		tmp.Close()

		return err
	}

	err = tmp.Close()
	if err != nil {
		return err
	}

	return os.Rename(tmp.Name(), target)
}
