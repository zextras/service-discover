/*
 * Copyright (C) 2023 Zextras srl
 *
 *     This program is free software: you can redistribute it and/or modify
 *     it under the terms of the GNU Affero General Public License as published by
 *     the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU Affero General Public License for more details.
 *
 *     You should have received a copy of the GNU Affero General Public License
 *     along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 */

package test

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"os"
	"testing"
)

const (
	LATEST_RELEASE       = "24.1.0"
	PUBLIC_IMAGE_ADDRESS = "carbonio/ce-directory-server-u20:%s"
	CI_DOCKER_NETWORK    = "ci_agent"
	CI_NETWORK_MODE      = "overlay"
)

// SpinUpCarbonioLdap launches a Carbonio LDAP instance with the desired version. It returns the LDAP instance context and the container itself. Note it is necessary to defer the container stop otherwise the instance will be hanging forever `defer ldapContainer.Terminate()`!
func SpinUpCarbonioLdap(t *testing.T, address string, version string) (testcontainers.Container, context.Context) {
	ctx := context.Background()

	var nets []string
	if os.Getenv("CI") == "true" {
		t.Log("Using " + CI_DOCKER_NETWORK + " as network for LDAP")
	} else {
		t.Log("Use standard local network for spinning LDAP")
	}
	t.Log("Networks that are going to be attached to the container")
	for _, nNet := range nets {
		t.Log(nNet)
	}
	req := testcontainers.ContainerRequest{
		Image:        fmt.Sprintf(address, version),
		ExposedPorts: []string{"389/tcp"},
		Entrypoint:   []string{"entrypoint"},
		WaitingFor:   wait.ForListeningPort("389/tcp"),
		Hostname:     "carbonio-ce-directory-server.carbonio-system.svc.cluster.local",
		AutoRemove:   true,
		Networks:     nets,
		NetworkMode:  container.NetworkMode("bridge"),
	}

	ldapC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	t.Log("LDAP Container created")
	logs, _ := ldapC.Logs(ctx)
	var logValues []byte
	logs.Read(logValues)
	t.Logf("%v", logValues)

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
