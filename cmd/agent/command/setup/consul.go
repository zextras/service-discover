// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import "github.com/hashicorp/consul/api"

type Client interface {
	Health() *api.Health
	Namespaces() *api.Namespaces
	Snapshot() *api.Snapshot
	LockKey(key string) (*api.Lock, error)
	LockOpts(opts *api.LockOptions) (*api.Lock, error)
	Connect() *api.Connect
	Event() *api.Event
	Coordinate() *api.Coordinate
	Debug() *api.Debug
	Session() *api.Session
	ConfigEntries() *api.ConfigEntries
	KV() *api.KV
	Txn() *api.Txn
	DiscoveryChain() *api.DiscoveryChain
	Agent() *api.Agent
	Operator() *api.Operator
	ACL() *api.ACL
	Catalog() *api.Catalog
	PreparedQuery() *api.PreparedQuery
	SemaphorePrefix(prefix string, limit int) (*api.Semaphore, error)
	SemaphoreOpts(opts *api.SemaphoreOptions) (*api.Semaphore, error)
	Raw() *api.Raw
	Status() *api.Status
}
