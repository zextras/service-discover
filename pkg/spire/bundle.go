// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// BundleVersion is the only join bundle version understood by this implementation.
const BundleVersion = 1

var (
	errBundleEncoding = errors.New("join bundle is not valid base64url")
	errBundleFormat   = errors.New("join bundle is not valid")
)

// JoinBundle contains everything a new node needs to join the cluster: how to reach and trust the SPIRE server, the
// one-time token to attest to it and what Consul needs to start in auto_config mode. It is a secret: the join token
// is valid until it expires or it is used.
type JoinBundle struct {
	Version       int      `json:"version"`
	TrustDomain   string   `json:"trust_domain"`
	SpireServer   string   `json:"spire_server"`
	SpireBundle   string   `json:"spire_bundle"`
	JoinToken     string   `json:"join_token"`
	ConsulCA      string   `json:"consul_ca"`
	ConsulServers []string `json:"consul_servers"`
	NodeName      string   `json:"node_name"`
}

// Encode serializes the bundle in the base64url (no padding) JSON form handed to the operator.
func (b *JoinBundle) Encode() (string, error) {
	err := b.validate()
	if err != nil {
		return "", err
	}

	bs, err := json.Marshal(b)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(bs), nil
}

// DecodeJoinBundle parses and validates a bundle produced by Encode. Surrounding whitespace is ignored.
func DecodeJoinBundle(encoded string) (*JoinBundle, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBundleEncoding, err)
	}

	bundle := &JoinBundle{}

	err = json.Unmarshal(decoded, bundle)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBundleFormat, err)
	}

	err = bundle.validate()
	if err != nil {
		return nil, err
	}

	return bundle, nil
}

func (b *JoinBundle) validate() error {
	switch {
	case b.Version != BundleVersion:
		return fmt.Errorf("%w: unsupported version %d", errBundleFormat, b.Version)
	case b.TrustDomain != TrustDomain:
		return fmt.Errorf("%w: unexpected trust domain %q", errBundleFormat, b.TrustDomain)
	case b.SpireServer == "":
		return fmt.Errorf("%w: missing spire_server", errBundleFormat)
	case b.SpireBundle == "":
		return fmt.Errorf("%w: missing spire_bundle", errBundleFormat)
	case b.JoinToken == "":
		return fmt.Errorf("%w: missing join_token", errBundleFormat)
	case b.ConsulCA == "":
		return fmt.Errorf("%w: missing consul_ca", errBundleFormat)
	case len(b.ConsulServers) == 0:
		return fmt.Errorf("%w: missing consul_servers", errBundleFormat)
	case b.NodeName == "":
		return fmt.Errorf("%w: missing node_name", errBundleFormat)
	}

	return nil
}
