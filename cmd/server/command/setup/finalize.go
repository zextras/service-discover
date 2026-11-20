// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/hashicorp/consul/api/v2"
	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/legacytoken"
	"github.com/zextras/service-discover/pkg/spire"
)

// Finalize revokes what the setup with the cluster credentials created in Consul for an agent that was migrated to
// SPIRE (setup --join --migrate on the agent), including the per-service tokens of the host that the services
// replaced with the tokens they get by logging in with SPIRE. It is idempotent.
type Finalize struct {
	setup Setup `kong:"-"`

	Host     string `required:"" help:"Hostname of the migrated agent, as given to join-token"`
	Password string `optional:"" help:"Password of the cluster credentials. If none is set, it is asked"`
}

type aclTokenEntry struct {
	AccessorID string `json:"AccessorID"`
	Policies   []struct {
		Name string `json:"Name"`
	} `json:"Policies"`
	NodeIdentities []struct {
		NodeName string `json:"NodeName"`
	} `json:"NodeIdentities"`
}

func (f *Finalize) Run(commonFlags *command.GlobalCommonFlags) error {
	password := f.Password

	if password == "" {
		var err error

		password, err = askClusterPassword()
		if err != nil {
			return err
		}
	}

	out, err := f.setup.finalizeAgent(realDependencies{}, f.Host, func() (clusterSecrets, error) {
		return f.setup.readClusterSecrets(password)
	})
	if err != nil {
		return err
	}

	render, err := formatter.Render(out, commonFlags.Format)
	if err != nil {
		return err
	}

	_, err = os.Stdout.WriteString(render)

	return err
}

// finalizeAgent deletes the ACL tokens carrying the policy of the node (the ones the tarball setup creates with
// CreateACLToken), the policy itself and the legacy per-service tokens of the host that SPIRE replaced (see
// legacytoken.Find for the conditions). Before that it checks that the node is alive in the cluster and owns the
// token issued by auto_config, so that the node is not left without a working token. When there is nothing left to
// delete it succeeds without touching anything.
//
//nolint:cyclop // sequential flow
func (s *Setup) finalizeAgent(
	deps businessDependencies, host string, readSecrets func() (clusterSecrets, error),
) (formatter.Formatter, error) {
	if deps.GetuidSyscall() != rootUID {
		return nil, errors.New("this command must be executed as root")
	}

	node := command.ConsulNodeName(command.Agent, host)
	out := &enableOutput{}

	secrets, err := readSecrets()
	if err != nil {
		return nil, err
	}

	err = os.Setenv(command.ConsulHTTPToken, secrets.token)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to set the Consul token")
	}
	defer os.Unsetenv(command.ConsulHTTPToken)

	tokens, err := consulJSON[[]aclTokenEntry](deps, "acl", "token", "list", "-format", "json")
	if err != nil {
		return nil, err
	}

	policies, err := consulJSON[[]struct {
		Name string `json:"Name"`
	}](deps, "acl", "policy", "list", "-format", "json")
	if err != nil {
		return nil, err
	}

	legacy, autoConfig := classifyAgentTokens(tokens, node)
	policyExists := false

	for _, policy := range policies {
		policyExists = policyExists || policy.Name == node
	}

	serviceTokens := &cliTokenACL{deps: deps}

	plan, err := legacytoken.Find(serviceTokens, legacytoken.NewSelector(command.Agent, host,
		func(service string) (bool, error) {
			return spire.NewServerCLI(deps.CreateCommand).HasEntry(spire.NodeID(host), spire.ServiceID(service))
		}))
	if err != nil {
		return nil, err
	}

	if len(legacy) == 0 && !policyExists && len(plan.Revocable) == 0 {
		out.add("Nothing to revoke for " + node + ": the legacy tokens and policy are already gone")
		addKeptServiceTokens(out, plan.Kept)

		return out, nil
	}

	if !autoConfig {
		return nil, errors.Errorf("the node %s has no token issued by auto_config: it did not join with SPIRE yet, "+
			"run 'service-discover setup --join <bundle> --migrate' on it first", node)
	}

	alive, err := nodeIsAlive(deps, node)
	if err != nil {
		return nil, err
	}

	if !alive {
		return nil, errors.Errorf("the node %s is not alive in the cluster: wait for it to rejoin", node)
	}

	for _, accessor := range legacy {
		_, err = deps.CreateCommand(command.ConsulBin, "acl", "token", "delete", "-id", accessor).Output()
		if err != nil {
			return nil, exec.ErrorFromStderr(err, "unable to delete the ACL token "+accessor)
		}
	}

	out.add("Deleted the legacy ACL tokens of " + node + ": " + strings.Join(legacy, ", "))

	if policyExists {
		_, err = deps.CreateCommand(command.ConsulBin, "acl", "policy", "delete", "-name", node).Output()
		if err != nil {
			return nil, exec.ErrorFromStderr(err, "unable to delete the ACL policy "+node)
		}

		out.add("Deleted the ACL policy " + node)
	}

	revoked, err := plan.Revoke(serviceTokens)
	for _, token := range revoked {
		out.add("Deleted the legacy ACL token " + token.AccessorID + " of " + token.Service)
	}

	if err != nil {
		return nil, err
	}

	addKeptServiceTokens(out, plan.Kept)

	return out, nil
}

func addKeptServiceTokens(out *enableOutput, kept []legacytoken.Kept) {
	for _, entry := range kept {
		out.add("Kept the legacy ACL token " + entry.Token.AccessorID + " of " + entry.Token.Service + ": " + entry.Reason)
	}
}

// cliTokenACL implements legacytoken.ACL with the consul binary and the token in CONSUL_HTTP_TOKEN.
type cliTokenACL struct {
	deps businessDependencies
}

func (c *cliTokenACL) TokenList() ([]*api.ACLTokenListEntry, error) {
	return consulJSON[[]*api.ACLTokenListEntry](c.deps, "acl", "token", "list", "-format", "json")
}

func (c *cliTokenACL) TokenDelete(accessorID string) error {
	_, err := c.deps.CreateCommand(command.ConsulBin, "acl", "token", "delete", "-id", accessorID).Output()
	if err != nil {
		return exec.ErrorFromStderr(err, "unable to delete the ACL token "+accessorID)
	}

	return nil
}

// classifyAgentTokens returns the accessors of the tokens bound to the policy named as the node, and whether the node
// owns a token issued by auto_config (the one with its node identity).
func classifyAgentTokens(tokens []aclTokenEntry, node string) ([]string, bool) {
	var legacy []string

	autoConfig := false

	for _, token := range tokens {
		for _, policy := range token.Policies {
			if policy.Name == node {
				legacy = append(legacy, token.AccessorID)

				break
			}
		}

		for _, identity := range token.NodeIdentities {
			autoConfig = autoConfig || identity.NodeName == node
		}
	}

	return legacy, autoConfig
}

// nodeIsAlive reads "consul members": the first column is the node and the third its status.
func nodeIsAlive(deps businessDependencies, node string) (bool, error) {
	members, err := deps.CreateCommand(command.ConsulBin, "members").Output()
	if err != nil {
		return false, exec.ErrorFromStderr(err, "unable to list the members of the cluster")
	}

	for line := range strings.SplitSeq(string(members), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == node && fields[2] == "alive" { //nolint:mnd // node, address, status
			return true, nil
		}
	}

	return false, nil
}

func consulJSON[T any](deps businessDependencies, args ...string) (T, error) {
	var result T

	content, err := deps.CreateCommand(command.ConsulBin, args...).Output()
	if err != nil {
		return result, exec.ErrorFromStderr(err, "unable to run consul "+strings.Join(args[:min(len(args), 3)], " "))
	}

	err = json.Unmarshal(content, &result)
	if err != nil {
		return result, errors.WithMessage(err, "unable to decode the answer of consul")
	}

	return result, nil
}
