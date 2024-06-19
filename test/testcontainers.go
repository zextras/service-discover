// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-units"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	LatestRelease      = "24.5.0"
	PublicImageAddress = "carbonio/ce-directory-server-u20:%s"
	CIDockerNetwork    = "ci_agent"
	CINetworkMode      = "overlay"
)

// SpinUpCarbonioLdap launches a Carbonio LDAP instance with the desired
// version. It returns the LDAP instance context and the container itself.
// Note it is necessary to defer the container stop otherwise the instance
// will be hanging forever `defer ldapContainer.Terminate()`!
func SpinUpCarbonioLdap(t *testing.T, address, version string) (testcontainers.Container, context.Context) {
	ctx := context.Background()

	var nets []string

	var netMode string

		t.Log("Using " + CIDockerNetwork + " as network for LDAP")
		nets = append(nets, CIDockerNetwork)
		netMode = CINetworkMode

	t.Log("Networks that are going to be attached to the container")

	for _, nNet := range nets {
		t.Log(nNet)
	}

	ulimits := []*units.Ulimit{{Name: "nofile", Soft: 32678, Hard: 32678}}
	req := testcontainers.ContainerRequest{
		Image:        fmt.Sprintf(address, version),
		ExposedPorts: []string{"389/tcp"},
		Entrypoint:   []string{"entrypoint"},
		WaitingFor:   wait.ForListeningPort("389/tcp"),
		Hostname:     "carbonio-ce-directory-server.carbonio-system.svc.cluster.local",
		HostConfigModifier: func(config *container.HostConfig) {
			config.AutoRemove = true
			config.NetworkMode = container.NetworkMode(netMode)
			config.Ulimits = ulimits
		},
		Networks: nets,
		ShmSize:  8 * 1024 * 1024 * 1024,
	}

	ldapC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})

	if err != nil {
		t.Fatal(err)
	}

	cip, _ := ldapC.ContainerIP(ctx)
	t.Log("Container ip: " + cip)

	listNets, _ := ldapC.Networks(ctx)

	for _, nName := range listNets {
		t.Log("Connected network: " + nName)
	}

	ports, _ := ldapC.Ports(ctx)

	for port, bindings := range ports {
		for _, binding := range bindings {
			t.Log("Port: " + port.Port() + " host bind: " + binding.HostPort + " ip bind: " + binding.HostIP)
		}
	}

	return ldapC, ctx
}
