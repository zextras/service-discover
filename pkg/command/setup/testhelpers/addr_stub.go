// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package testhelpers

// AddrStub is a test helper that implements net.Addr for testing purposes.
type AddrStub struct {
	IP string
}

// Network returns the network type (always "tcp" for this stub).
func (a *AddrStub) Network() string {
	return "tcp"
}

// String returns the IP address string.
func (a *AddrStub) String() string {
	return a.IP
}
