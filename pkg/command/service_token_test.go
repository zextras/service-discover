// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"errors"
	"testing"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeServiceTokenACL struct {
	node    string
	tokens  map[string]*api.ACLToken
	readErr error
	created []*api.ACLToken
	updated []*api.ACLToken
	deleted []string
}

func (f *fakeServiceTokenACL) NodeName() (string, error) { return f.node, nil }

func (f *fakeServiceTokenACL) TokenRead(id string) (*api.ACLToken, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}

	token, ok := f.tokens[id]
	if !ok {
		return nil, errServiceTokenNotFound
	}

	return token, nil
}

func (f *fakeServiceTokenACL) TokenCreate(t *api.ACLToken) (*api.ACLToken, error) {
	f.created = append(f.created, t)
	out := *t
	out.SecretID = "8b1f0c7e-1d1a-4a59-9a2e-6f3c0b7e2d11"

	return &out, nil
}

func (f *fakeServiceTokenACL) TokenUpdate(t *api.ACLToken) (*api.ACLToken, error) {
	f.updated = append(f.updated, t)

	return t, nil
}

func (f *fakeServiceTokenACL) TokenListByPolicy(string) ([]*api.ACLTokenListEntry, error) {
	var out []*api.ACLTokenListEntry
	for _, t := range f.tokens {
		out = append(out, &api.ACLTokenListEntry{AccessorID: t.AccessorID, Description: t.Description})
	}

	return out, nil
}

func (f *fakeServiceTokenACL) TokenDelete(id string) error {
	f.deleted = append(f.deleted, id)

	return nil
}

const (
	testService = "carbonio-files"
	testPolicy  = "carbonio-files-policy"
	testNode    = "server-mail1-example-com"
)

func TestServiceTokenAccessorIDIsStablePerServiceAndNode(t *testing.T) {
	// Pinned value (RFC 4122 UUIDv5, cross-checked with Python uuid.uuid5): shared
	// with carbonio-quarkus-extensions and existing clusters; must never change.
	assert.Equal(t, "13a68748-7a84-55a8-884c-f4bb1f05442f", ServiceTokenAccessorID(testService, testNode))
	assert.NotEqual(t, ServiceTokenAccessorID(testService, testNode),
		ServiceTokenAccessorID(testService, "server-mail2-example-com"))
	assert.NotEqual(t, ServiceTokenAccessorID(testService, testNode),
		ServiceTokenAccessorID("carbonio-preview", testNode))
}

func TestEnsureServiceTokenCreatesWithDeterministicAccessor(t *testing.T) {
	acl := &fakeServiceTokenACL{node: testNode}

	secret, err := ensureServiceToken(acl, testService, testPolicy)

	require.NoError(t, err)
	assert.Equal(t, "8b1f0c7e-1d1a-4a59-9a2e-6f3c0b7e2d11", secret)
	require.Len(t, acl.created, 1)
	assert.Equal(t, ServiceTokenAccessorID(testService, testNode), acl.created[0].AccessorID)
	assert.Equal(t, "carbonio/carbonio-files/server-mail1-example-com", acl.created[0].Description)
	assert.Equal(t, testPolicy, acl.created[0].Policies[0].Name)
}

func TestEnsureServiceTokenReusesExistingToken(t *testing.T) {
	id := ServiceTokenAccessorID(testService, testNode)
	acl := &fakeServiceTokenACL{node: testNode, tokens: map[string]*api.ACLToken{id: {
		AccessorID: id,
		SecretID:   "3c9a7f52-0d5e-4b8e-a7f1-2b6d9e0c4a13",
		Policies:   []*api.ACLTokenPolicyLink{{Name: testPolicy}},
	}}}

	secret, err := ensureServiceToken(acl, testService, testPolicy)

	require.NoError(t, err)
	assert.Equal(t, "3c9a7f52-0d5e-4b8e-a7f1-2b6d9e0c4a13", secret)
	assert.Empty(t, acl.created)
	assert.Empty(t, acl.updated)
}

func TestEnsureServiceTokenRelinksMissingPolicy(t *testing.T) {
	id := ServiceTokenAccessorID(testService, testNode)
	acl := &fakeServiceTokenACL{node: testNode, tokens: map[string]*api.ACLToken{id: {
		AccessorID: id,
		SecretID:   "3c9a7f52-0d5e-4b8e-a7f1-2b6d9e0c4a13",
	}}}

	_, err := ensureServiceToken(acl, testService, testPolicy)

	require.NoError(t, err)
	require.Len(t, acl.updated, 1)
	assert.Equal(t, testPolicy, acl.updated[0].Policies[0].Name)
}

func TestEnsureServiceTokenRejectsRedactedSecret(t *testing.T) {
	id := ServiceTokenAccessorID(testService, testNode)
	acl := &fakeServiceTokenACL{node: testNode, tokens: map[string]*api.ACLToken{id: {
		AccessorID: id,
		SecretID:   "<hidden>",
	}}}

	_, err := ensureServiceToken(acl, testService, testPolicy)

	assert.ErrorContains(t, err, "acl:write")
}

func TestEnsureServiceTokenNeverCreatesOnReadError(t *testing.T) {
	acl := &fakeServiceTokenACL{node: testNode, readErr: errors.New("connection refused")}

	_, err := ensureServiceToken(acl, testService, testPolicy)

	require.Error(t, err)
	assert.Empty(t, acl.created)
}

func TestPruneLegacyServiceTokensDeletesOnlyThisNodeLegacyTokens(t *testing.T) {
	current := ServiceTokenAccessorID(testService, testNode)
	tokens := map[string]*api.ACLToken{
		current:          {AccessorID: current, Description: "carbonio/carbonio-files/" + testNode},
		"bash-this-host": {AccessorID: "bash-this-host", Description: "Token for\n    carbonio-files/mail1.example.com mail1 "},
		"java-this-host": {AccessorID: "java-this-host", Description: "carbonio-files/mail1.example.com"},
		"bash-mail10":    {AccessorID: "bash-mail10", Description: "Token for\n    carbonio-files/mail10.example.com "},
		"other-service":  {AccessorID: "other-service", Description: "Token for carbonio-files-db/mail1.example.com"},
		"unrelated":      {AccessorID: "unrelated", Description: "admin token for mail1.example.com"},
		"typo-alias":     {AccessorID: "typo-alias", Description: "Token for\n    carobonio-files/mail1.example.com "},
	}
	acl := &fakeServiceTokenACL{node: testNode, tokens: tokens}

	deleted, err := pruneLegacyServiceTokens(acl, testService, []string{testService, "carobonio-files"}, testPolicy,
		[]string{"mail1", "mail1.example.com"})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"bash-this-host", "java-this-host", "typo-alias"}, deleted)
}

func TestIsTokenNotFound(t *testing.T) {
	assert.True(t, isTokenNotFound(api.StatusError{Code: 404, Body: "Token not found"}))
	assert.True(t, isTokenNotFound(api.StatusError{Code: 403, Body: "ACL not found"}))
	assert.False(t, isTokenNotFound(api.StatusError{Code: 403, Body: "Permission denied"}))
	assert.False(t, isTokenNotFound(api.StatusError{Code: 500, Body: "rpc error"}))
	assert.False(t, isTokenNotFound(errors.New("dial tcp: connection refused")))
}
