// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zextras/service-discover/pkg/exec"
)

var errJWTNotFound = errors.New("spire-agent did not return a JWT-SVID")

// AgentCLI drives the spire-agent binary through the Workload API socket of the local agent.
type AgentCLI struct {
	Command    func(name string, args ...string) exec.Cmd
	SocketPath string
}

// NewAgentCLI returns an AgentCLI talking to the default SPIRE agent socket.
func NewAgentCLI(command func(name string, args ...string) exec.Cmd) *AgentCLI {
	return &AgentCLI{Command: command, SocketPath: AgentSocket}
}

// FetchJWTSVID asks the local agent for a JWT-SVID of spiffeID with the given audience.
func (a *AgentCLI) FetchJWTSVID(audience, spiffeID string) (string, error) {
	out, err := a.Command(AgentBin, "api", "fetch", "jwt",
		"-audience", audience, "-spiffeID", spiffeID, "-socketPath", a.SocketPath).Output()
	if err != nil {
		return "", exec.ErrorFromStderr(err, "unable to fetch a JWT-SVID for "+spiffeID)
	}

	return parseJWTSVID(string(out))
}

// WaitJWTSVID fetches the JWT-SVID retrying until the Workload API answers (the agent has attested itself and
// synchronized its entries) or the timeout expires.
func (a *AgentCLI) WaitJWTSVID(audience, spiffeID string, timeout, interval time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for {
		token, err := a.FetchJWTSVID(audience, spiffeID)
		if err == nil {
			return token, nil
		}

		if time.Now().After(deadline) {
			return "", fmt.Errorf("the SPIRE Workload API is not ready: %w", err)
		}

		time.Sleep(interval)
	}
}

// parseJWTSVID extracts the token from the pretty output of `spire-agent api fetch jwt`: the token is on the second
// line, after the `token(<spiffe id>):` header.
func parseJWTSVID(output string) (string, error) {
	lines := strings.Split(output, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "token(") {
		return "", errJWTNotFound
	}

	token := strings.TrimSpace(lines[1])
	if token == "" {
		return "", errJWTNotFound
	}

	return token, nil
}
