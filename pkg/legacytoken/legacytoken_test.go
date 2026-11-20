// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package legacytoken_test

import (
	"errors"
	"testing"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/legacytoken"
)

type fakeACL struct {
	tokens    []*api.ACLTokenListEntry
	deleted   []string
	listErr   error
	deleteErr map[string]error
}

func (f *fakeACL) TokenList() ([]*api.ACLTokenListEntry, error) { return f.tokens, f.listErr }

func (f *fakeACL) TokenDelete(accessorID string) error {
	if err := f.deleteErr[accessorID]; err != nil {
		return err
	}

	f.deleted = append(f.deleted, accessorID)

	return nil
}

func legacy(id, description string, policies ...string) *api.ACLTokenListEntry {
	entry := &api.ACLTokenListEntry{AccessorID: id, Description: description}
	for _, policy := range policies {
		entry.Policies = append(entry.Policies, &api.ACLTokenPolicyLink{Name: policy})
	}

	return entry
}

func login(id string, roles ...string) *api.ACLTokenListEntry {
	entry := &api.ACLTokenListEntry{AccessorID: id, AuthMethod: "spire", Local: true, Description: "token created via login"}
	for _, role := range roles {
		entry.Roles = append(entry.Roles, &api.ACLTokenRoleLink{Name: role})
	}

	return entry
}

const host = "mail.example.com"

func selector() legacytoken.Selector {
	return legacytoken.NewSelector(command.Server, host, nil)
}

func TestRevokeLegacyServiceTokens(t *testing.T) {
	acl := &fakeACL{tokens: []*api.ACLTokenListEntry{
		// created by the deterministic scheme and by the bash pending-setups, in the formats seen in production
		legacy("files", "carbonio/carbonio-files/server-mail-example-com", "carbonio-files-policy"),
		legacy("catalog", "Token for\n    carbonio-catalog/mail.example.com ", "carbonio-catalog-policy"),
		legacy("multi", "Token for carbonio-mta/other.example.com mail.example.com. ", "carbonio-mta-policy"),
		login("l-files", "carbonio-files-role"),
		login("l-catalog", "carbonio-catalog-role"),
		login("l-mta", "carbonio-mta-role"),
		// not attributable to this host
		legacy("other-node", "carbonio/carbonio-files/server-other-example-com", "carbonio-files-policy"),
		legacy("other-host", "Token for carbonio-catalog/mail2.example.com", "carbonio-catalog-policy"),
		legacy("suffix-host", "Token for carbonio-catalog/xmail.example.com", "carbonio-catalog-policy"),
		// not service tokens
		legacy("node", "", "server-mail-example-com"),
		legacy("bootstrap", "Bootstrap Token (Global Management)", "global-management"),
		legacy("anonymous", "Anonymous Token"),
		// the description names another service than the policy
		legacy("mismatch", "carbonio/carbonio-auth/server-mail-example-com", "carbonio-files-policy"),
		// the description of a legacy token has to name this service for this host
		legacy("no-host", "Token for carbonio-files", "carbonio-files-policy"),
		legacy("other-service", "Token for carbonio-auth/mail.example.com", "carbonio-files-policy"),
		// more than the service policy
		legacy("extra-policy", "carbonio/carbonio-files/server-mail-example-com", "carbonio-files-policy", "global-management"),
	}}

	revoked, kept, err := legacytoken.RevokeLegacyServiceTokens(acl, selector())
	require.NoError(t, err)

	assert.Equal(t, []string{"files", "catalog", "multi"}, acl.deleted)
	assert.Len(t, revoked, 3)
	assert.Equal(t, legacytoken.Token{
		AccessorID: "files", Service: "carbonio-files", Description: "carbonio/carbonio-files/server-mail-example-com",
	}, revoked[0])
	assert.Empty(t, kept)

	// idempotent: once deleted the tokens are not listed
	acl.deleted = nil
	acl.tokens = acl.tokens[3:]

	revoked, kept, err = legacytoken.RevokeLegacyServiceTokens(acl, selector())
	require.NoError(t, err)
	assert.Empty(t, revoked)
	assert.Empty(t, kept)
	assert.Empty(t, acl.deleted)
}

func TestRevokeLegacyServiceTokens_keepsTheTokenOfAServiceThatDidNotLogIn(t *testing.T) {
	acl := &fakeACL{tokens: []*api.ACLTokenListEntry{
		legacy("files", "carbonio/carbonio-files/server-mail-example-com", "carbonio-files-policy"),
		legacy("auth", "carbonio/carbonio-auth/server-mail-example-com", "carbonio-auth-policy"),
		// a login of another auth method, of another service role or one that is not a login does not count
		{AccessorID: "jwt", AuthMethod: "other", Roles: []*api.ACLTokenRoleLink{{Name: "carbonio-files-role"}}},
		login("l-other", "carbonio-mta-role"),
		{AccessorID: "no-role", AuthMethod: "spire"},
		legacy("l-policy", "x", "carbonio-files-role"),
	}}

	revoked, kept, err := legacytoken.RevokeLegacyServiceTokens(acl, selector())
	require.NoError(t, err)

	assert.Empty(t, revoked)
	assert.Empty(t, acl.deleted)
	require.Len(t, kept, 2)
	assert.Equal(t, "files", kept[0].Token.AccessorID)
	assert.Contains(t, kept[0].Reason, "carbonio-files-role")
	assert.Equal(t, "auth", kept[1].Token.AccessorID)
}

func TestRevokeLegacyServiceTokens_checksTheSPIREEntry(t *testing.T) {
	acl := &fakeACL{tokens: []*api.ACLTokenListEntry{
		legacy("files", "carbonio/carbonio-files/server-mail-example-com", "carbonio-files-policy"),
		legacy("catalog", "Token for carbonio-catalog/mail.example.com", "carbonio-catalog-policy"),
		login("l-files", "carbonio-files-role"),
		login("l-catalog", "carbonio-catalog-role"),
	}}

	var asked []string

	selector := legacytoken.NewSelector(command.Server, host, func(service string) (bool, error) {
		asked = append(asked, service)

		return service == "carbonio-files", nil
	})

	revoked, kept, err := legacytoken.RevokeLegacyServiceTokens(acl, selector)
	require.NoError(t, err)

	assert.Equal(t, []string{"files"}, acl.deleted)
	assert.Len(t, revoked, 1)
	assert.Equal(t, []string{"carbonio-files", "carbonio-catalog"}, asked)
	require.Len(t, kept, 1)
	assert.Equal(t, "catalog", kept[0].Token.AccessorID)
	assert.Contains(t, kept[0].Reason, "spiffe://carbonio/service/carbonio-catalog")
}

func TestRevokeLegacyServiceTokens_errors(t *testing.T) {
	t.Run("list failure", func(t *testing.T) {
		_, _, err := legacytoken.RevokeLegacyServiceTokens(&fakeACL{listErr: errors.New("denied")}, selector())
		require.ErrorContains(t, err, "denied")
	})

	t.Run("SPIRE failure deletes nothing", func(t *testing.T) {
		acl := &fakeACL{tokens: []*api.ACLTokenListEntry{
			legacy("files", "carbonio/carbonio-files/server-mail-example-com", "carbonio-files-policy"),
			login("l-files", "carbonio-files-role"),
		}}

		_, _, err := legacytoken.RevokeLegacyServiceTokens(acl, legacytoken.NewSelector(command.Server, host,
			func(string) (bool, error) { return false, errors.New("spire down") }))
		require.ErrorContains(t, err, "spire down")
		assert.Empty(t, acl.deleted)
	})

	t.Run("delete failure reports what was deleted", func(t *testing.T) {
		acl := &fakeACL{
			tokens: []*api.ACLTokenListEntry{
				legacy("files", "carbonio/carbonio-files/server-mail-example-com", "carbonio-files-policy"),
				legacy("auth", "carbonio/carbonio-auth/server-mail-example-com", "carbonio-auth-policy"),
				login("l-files", "carbonio-files-role"),
				login("l-auth", "carbonio-auth-role"),
			},
			deleteErr: map[string]error{"auth": errors.New("boom")},
		}

		revoked, _, err := legacytoken.RevokeLegacyServiceTokens(acl, selector())
		require.ErrorContains(t, err, "boom")
		require.Len(t, revoked, 1)
		assert.Equal(t, "files", revoked[0].AccessorID)
	})
}
