// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package legacytoken

import (
	"github.com/hashicorp/consul/api/v2"
)

type clientACL struct {
	client *api.Client
}

// NewClientACL returns the ACL backed by a Consul API client, whose token needs acl:write.
func NewClientACL(client *api.Client) ACL {
	return &clientACL{client: client}
}

func (c *clientACL) TokenList() ([]*api.ACLTokenListEntry, error) {
	tokens, _, err := c.client.ACL().TokenList(nil)

	return tokens, err
}

func (c *clientACL) TokenDelete(accessorID string) error {
	_, err := c.client.ACL().TokenDelete(accessorID, nil)

	return err
}
