// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	sharedsetup "github.com/zextras/service-discover/pkg/command/setup"
)

func TestRealDependencies_LookupIP(t *testing.T) {
	t.Parallel()

	deps := sharedsetup.RealDependencies{}

	t.Run("Should resolve localhost successfully", func(t *testing.T) {
		ips, err := deps.LookupIP("localhost")
		assert.NoError(t, err)
		assert.NotEmpty(t, ips)

		// Check that we got at least one valid IP
		hasValidIP := false
		for _, ip := range ips {
			if ip.IsLoopback() {
				hasValidIP = true
				break
			}
		}
		assert.True(t, hasValidIP, "Expected at least one loopback IP")
	})

	t.Run("Should fail for invalid hostname", func(t *testing.T) {
		ips, err := deps.LookupIP("this-hostname-should-not-exist-12345.invalid")
		assert.Error(t, err)
		assert.Nil(t, ips)
	})

	t.Run("Should resolve IP address directly", func(t *testing.T) {
		ips, err := deps.LookupIP("127.0.0.1")
		assert.NoError(t, err)
		assert.NotEmpty(t, ips)
		assert.True(t, ips[0].Equal(net.IPv4(127, 0, 0, 1)))
	})
}
