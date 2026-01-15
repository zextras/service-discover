// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package test

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-units"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	LatestRelease      = "latest"
	PublicImageAddress = "registry.dev.zextras.com/dev/carbonio-openldap:%s"
)

type LdapContainer struct {
	Stop func()
	URL  func() string
	Ip   func() string
	Port func() string
}

// SpinUpCarbonioLdap launches a Carbonio LDAP instance with the desired
// version. It returns the LDAP instance context and the container itself.
// Note it is necessary to defer the container stop otherwise the instance
// will be hanging forever `defer ldapContainer.Terminate()`!
func SpinUpCarbonioLdap(t *testing.T, address, version string) (LdapContainer, context.Context) {
	ctx := context.Background()

	t.Log("Networks that are going to be attached to the container")

	ulimits := []*units.Ulimit{{Name: "nofile", Soft: 32678, Hard: 32678}}
	req := testcontainers.ContainerRequest{
		Image:        fmt.Sprintf(address, version),
		ExposedPorts: []string{"1389/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForLog("modifying entry \"uid=zimbra,cn=admins,cn=zimbra\""),
			wait.ForListeningPort("1389/tcp"),
		),
		HostConfigModifier: func(config *container.HostConfig) {
			config.AutoRemove = true
			config.Ulimits = ulimits
		},
		ShmSize: 8 * 1024 * 1024 * 1024,
	}

	ldapContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})

	if err != nil {
		t.Fatal(err)
	}

	cip, _ := ldapContainer.ContainerIP(ctx)
	t.Log("Container ip: " + cip)

	ports, _ := ldapContainer.Ports(ctx)

	for port, bindings := range ports {
		for _, binding := range bindings {
			t.Log("Port: " + port.Port() + " host bind: " + binding.HostPort + " ip bind: " + binding.HostIP)
		}
	}

	containerWithPort := LdapContainer{
		Stop: func() {
			err := ldapContainer.Terminate(ctx)
			if err != nil {
				t.Log(err)
			}
		},
		Ip: func() string {
			return "localhost"
		},
		Port: func() string {
			port, _ := ldapContainer.MappedPort(ctx, "1389")
			return port.Port()
		},
	}
	containerWithPort.URL = func() string {
		return "ldap://localhost:" + containerWithPort.Port()
	}
	return containerWithPort, ctx
}
