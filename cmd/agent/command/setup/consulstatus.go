// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

const (
	// consulLocalAPI is the HTTP API of the local Consul agent. The status endpoints need no ACL token.
	consulLocalAPI       = "http://127.0.0.1:8500"
	consulStatusTimeout  = 5 * time.Second
	maxConsulStatusReply = 1 << 16
)

// consulStatusLeader returns the address of the Raft leader seen by the local Consul agent: it is empty, or an
// error, while the agent has not joined the servers. It is a variable for the tests.
var consulStatusLeader = func() (string, error) {
	var leader string

	err := consulStatusGet("/v1/status/leader", &leader)

	return leader, err
}

// consulStatusPeers returns the address of the Consul servers (Raft peers) seen by the local Consul agent. It is a
// variable for the tests.
var consulStatusPeers = func() ([]string, error) {
	var peers []string

	err := consulStatusGet("/v1/status/peers", &peers)

	return peers, err
}

func consulStatusGet(path string, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), consulStatusTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, consulLocalAPI+path, http.NoBody)
	if err != nil {
		return err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errors.Errorf("the local Consul agent answered %s to %s", response.Status, path)
	}

	content, err := io.ReadAll(io.LimitReader(response.Body, maxConsulStatusReply))
	if err != nil {
		return err
	}

	return json.Unmarshal(content, out)
}
