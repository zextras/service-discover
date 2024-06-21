// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package test

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-units"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"testing"
)

const (
	LatestRelease      = "24.5.0"
	PublicImageAddress = "carbonio/ce-directory-server-u20:%s"
)

// SpinUpCarbonioLdap launches a Carbonio LDAP instance with the desired
// version. It returns the LDAP instance context and the Container itself.
// Note it is necessary to defer the Container stop otherwise the instance
// will be hanging forever `defer ldapContainer.Terminate()`!
func SpinUpCarbonioLdap(t *testing.T, address, version string) (*LdapContainer, context.Context) {
	ctx := context.Background()

	var nets []string

	var netMode string

	t.Log("Networks that are going to be attached to the Container")

	for _, nNet := range nets {
		t.Log(nNet)
	}

	ulimits := []*units.Ulimit{{Name: "nofile", Soft: 32678, Hard: 32678}}
	req := testcontainers.ContainerRequest{
		Image:        fmt.Sprintf(address, version),
		ExposedPorts: []string{"389/tcp"},
		Entrypoint:   []string{"entrypoint"},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("389/tcp"),
			wait.ForLog("Starting directory server...Done."),
			wait.ForLog("Starting config service...Done."),
			wait.ForLog("Starting stats...Done."),
		),
		Hostname: "carbonio-ce-directory-server.carbonio-system.svc.cluster.local",
		HostConfigModifier: func(config *container.HostConfig) {
			config.AutoRemove = true
			config.NetworkMode = container.NetworkMode(netMode)
			config.Ulimits = ulimits
		},
		ShmSize: 8 * 1024 * 1024 * 1024,
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Opts:      []testcontainers.LogProductionOption{},
			Consumers: []testcontainers.LogConsumer{&ContainerTestingLogConsumer{t: t}},
		},
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
	var ldapContainer = &LdapContainer{Container: ldapC}

	return ldapContainer, ctx
}

type ContainerTestingLogConsumer struct {
	t *testing.T
}

type LdapContainer struct {
	Container testcontainers.Container
}

func (consumer *ContainerTestingLogConsumer) Accept(l testcontainers.Log) {
	consumer.t.Log(string(l.Content))
}

func (ldapContainer *LdapContainer) GetHostLdapUrl(containerCtx context.Context) (string, error) {
	port, err := ldapContainer.Container.MappedPort(containerCtx, "389")
	return fmt.Sprintf("ldap://%s:%s", ldapContainer.GetHostIp(), port.Port()), err
}

func (ldapContainer *LdapContainer) GetHostIp() string {
	return "127.0.0.1"
}
