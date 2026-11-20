// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"os"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command/setup/mocks"
	mocksexec "github.com/zextras/service-discover/pkg/exec/mocks"
)

const finalizeNode = "agent-mail2-example-com"

const membersHeader = "Node  Address  Status  Type  Build  Protocol  DC  Partition  Segment\n"

type finalizeFixture struct {
	deps    *mocks.BusinessDependencies
	deleted *[]string
}

func newFinalizeFixture(t *testing.T, tokens, policies, members string) *finalizeFixture {
	t.Helper()

	output := func(out string, err error) *mocksexec.Cmd {
		cmd := new(mocksexec.Cmd)
		cmd.On("Output").Return([]byte(out), err)

		return cmd
	}

	deleted := &[]string{}
	deps := new(mocks.BusinessDependencies)
	deps.On("GetuidSyscall").Return(0).
		On("CreateCommand", consulBin, "acl", "token", "list", "-format", "json").Return(output(tokens, nil)).
		On("CreateCommand", consulBin, "acl", "policy", "list", "-format", "json").Return(output(policies, nil)).
		On("CreateCommand", consulBin, "members").Return(output(members, nil)).
		On("CreateCommand", consulBin, "acl", "token", "delete", "-id", mock.Anything).
		Run(func(args mock.Arguments) { *deleted = append(*deleted, "token:"+args.String(5)) }).
		Return(output("", nil)).
		On("CreateCommand", consulBin, "acl", "policy", "delete", "-name", mock.Anything).
		Run(func(args mock.Arguments) { *deleted = append(*deleted, "policy:"+args.String(5)) }).
		Return(output("", nil))

	return &finalizeFixture{deps: deps, deleted: deleted}
}

func finalizeSecrets() (clusterSecrets, error) { return clusterSecrets{token: "root"}, nil }

const (
	legacyToken = `{"AccessorID":"legacy-1","Policies":[{"Name":"` + finalizeNode + `"}]}`
	autoToken   = `{"AccessorID":"auto-1","NodeIdentities":[{"NodeName":"` + finalizeNode + `"}]}`
	otherToken  = `{"AccessorID":"other-1","Policies":[{"Name":"agent-someone-else"}]}`
	policyList  = `[{"Name":"global-management"},{"Name":"` + finalizeNode + `"}]`
)

func TestSetup_finalizeAgent(t *testing.T) {
	f := newFinalizeFixture(t, "["+legacyToken+","+autoToken+","+otherToken+"]", policyList,
		membersHeader+finalizeNode+" 10.0.0.9:8301 alive client 2.1.0 2 dc1 default <default>\n")

	out, err := (&Setup{}).finalizeAgent(f.deps, "mail2.example.com", finalizeSecrets)
	require.NoError(t, err)

	assert.Equal(t, []string{"token:legacy-1", "policy:" + finalizeNode}, *f.deleted,
		"only the legacy token and policy of the node: not the auto_config token nor other nodes")
	assert.Empty(t, os.Getenv("CONSUL_HTTP_TOKEN"))

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "legacy-1")
}

func TestSetup_finalizeAgent_idempotent(t *testing.T) {
	f := newFinalizeFixture(t, "["+autoToken+","+otherToken+"]", `[{"Name":"global-management"}]`, membersHeader)

	out, err := (&Setup{}).finalizeAgent(f.deps, "mail2.example.com", finalizeSecrets)
	require.NoError(t, err)
	assert.Empty(t, *f.deleted)

	plain, err := out.PlainRender()
	require.NoError(t, err)
	assert.Contains(t, plain, "Nothing to revoke")
}

func TestSetup_finalizeAgent_resumesAfterAPartialRun(t *testing.T) {
	// the tokens were deleted but the policy was not
	f := newFinalizeFixture(t, "["+autoToken+"]", policyList,
		membersHeader+finalizeNode+" 10.0.0.9:8301 alive client 2.1.0 2 dc1 default <default>\n")

	_, err := (&Setup{}).finalizeAgent(f.deps, "mail2.example.com", finalizeSecrets)
	require.NoError(t, err)
	assert.Equal(t, []string{"policy:" + finalizeNode}, *f.deleted)
}

func TestSetup_finalizeAgent_refusesWhileTheNodeIsNotOnSPIRE(t *testing.T) {
	alive := membersHeader + finalizeNode + " 10.0.0.9:8301 alive client 2.1.0 2 dc1 default <default>\n"

	t.Run("no auto_config token", func(t *testing.T) {
		f := newFinalizeFixture(t, "["+legacyToken+"]", policyList, alive)

		_, err := (&Setup{}).finalizeAgent(f.deps, "mail2.example.com", finalizeSecrets)
		require.ErrorContains(t, err, "no token issued by auto_config")
		assert.Empty(t, *f.deleted)
	})

	t.Run("node not alive", func(t *testing.T) {
		f := newFinalizeFixture(t, "["+legacyToken+","+autoToken+"]", policyList,
			membersHeader+finalizeNode+" 10.0.0.9:8301 failed client 2.1.0 2 dc1 default <default>\n")

		_, err := (&Setup{}).finalizeAgent(f.deps, "mail2.example.com", finalizeSecrets)
		require.ErrorContains(t, err, "not alive")
		assert.Empty(t, *f.deleted)
	})

	t.Run("secrets unreadable", func(t *testing.T) {
		f := newFinalizeFixture(t, "[]", "[]", membersHeader)

		_, err := (&Setup{}).finalizeAgent(f.deps, "mail2.example.com",
			func() (clusterSecrets, error) { return clusterSecrets{}, errors.New("bad password") })
		require.ErrorContains(t, err, "bad password")
	})
}
