// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errCommand = errors.New("command failed")

// hostAnswers makes the machine answer the commands that tell about the host.
func (m *fakeMachine) hostAnswers(answers map[string]string, others func(*Command) ([]byte, error)) {
	m.exec = func(cmd *Command) ([]byte, error) {
		if answer, found := answers[cmd.Name+" "+strings.Join(cmd.Args, " ")]; found {
			return []byte(answer + "\n"), nil
		}

		if others != nil {
			return others(cmd)
		}

		return nil, nil
	}
}

func (m *fakeMachine) writeServiceFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func (m *fakeMachine) serviceDir(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(m.env.CarbonioDir, dir, "service-discover"), 0o755))
}

func TestSync_setupRunsTheCommandsOfAGeneratedServiceInOrder(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.users["videoserver"] = Account{UID: 1020, GID: 1020}
	machine.serviceDir(t, "videoserver")

	var restartsAtRouting []string

	machine.hostAnswers(map[string]string{"hostname -I": "10.0.0.5 10.0.0.6"}, func(cmd *Command) ([]byte, error) {
		switch cmd.Name {
		case "carbonio-videoserver-configs":
			machine.writeServiceFile(t, filepath.Join(machine.env.ServicesDir, "carbonio-videoserver.hcl"),
				"services {\n  name = \"carbonio-videoserver\"\n}\n")
		case "carbonio-videoserver-routing":
			restartsAtRouting = machine.restarts()
		}

		return nil, nil
	})

	out := &bytes.Buffer{}

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{
		Services: []string{"carbonio-videoserver"}, Setup: true, ForceRestart: true, Out: out,
	}))

	var names []string

	for _, cmd := range machine.commands {
		if cmd.Name != "hostname" {
			names = append(names, cmd.Name)
		}
	}

	assert.Equal(t, []string{"carbonio-videoserver-configs", "carbonio-videoserver-routing"}, names)

	for _, cmd := range machine.commands {
		if cmd.Name == "hostname" {
			continue
		}

		assert.Contains(t, cmd.Env, "CONSUL_HTTP_TOKEN=secret-1")
		assert.Contains(t, cmd.Env, "SETUP_CONSUL_TOKEN=secret-1")
		assert.Contains(t, cmd.Env, "LOCAL_IP=10.0.0.5")
	}

	rendered, err := os.ReadFile(filepath.Join(machine.env.RenderedDir, "carbonio-videoserver.hcl"))
	require.NoError(t, err, "the HCL the command produced is picked up")
	assert.Contains(t, string(rendered), `token = "secret-1"`)

	assert.Equal(t, 1, machine.calls("reload"))
	assert.Equal(t, []string{"carbonio-videoserver-sidecar.service", "carbonio-videoserver.service"}, restartsAtRouting,
		"the routing waits for the service to be restarted")
}

func TestSync_setupFailingCommandStopsTheServiceOnly(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.users["videoserver"] = Account{UID: 1020, GID: 1020}
	machine.serviceDir(t, "videoserver")
	machine.installFiles(t)

	machine.exec = func(cmd *Command) ([]byte, error) {
		if cmd.Name == "carbonio-videoserver-configs" {
			return nil, errCommand
		}

		return nil, nil
	}

	err := Sync(machine.env, loadDefs(t), SyncOptions{Setup: true, ForceRestart: true})
	require.ErrorIs(t, err, errCommand)
	assert.Contains(t, machine.restarts(), "carbonio-files.service", "the other services go on")

	for _, cmd := range machine.commands {
		assert.NotEqual(t, "carbonio-videoserver-routing", cmd.Name, "a service that failed is not routed")
	}
}

func TestSync_withoutSetupNoCommandRuns(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.users["videoserver"] = Account{UID: 1020, GID: 1020}
	machine.serviceDir(t, "videoserver")

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))
	assert.Empty(t, machine.commands)
	assert.Empty(t, machine.restarts()[:0])
}

func TestSync_forceRestartRestartsWhatDidNotChange(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	opts := SyncOptions{Services: []string{"carbonio-files"}}

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))

	machine.systemctl = nil

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))
	assert.Empty(t, machine.restarts())
	assert.Zero(t, machine.calls("reload"))

	opts.ForceRestart = true

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))
	assert.Equal(t, []string{"carbonio-files-sidecar.service", "carbonio-files-watches.service", "carbonio-files.service"},
		machine.restarts())
	assert.Equal(t, 1, machine.calls("reload"))
}

const docsEditorTemplate = `services {
  name = "carbonio-docs-editor"
  meta = {
    $METADATA_KEY = "$METADATA_VALUE"
  }
}
`

func docsEditorMachine(t *testing.T) *fakeMachine {
	t.Helper()

	machine := newMachine(t)
	machine.users["carbonio-docs-editor"] = Account{UID: 1030, GID: 1030}
	machine.serviceDir(t, "docs-editor")
	machine.writeServiceFile(t, filepath.Join(machine.env.ServicesDir, "templates", "carbonio-docs-editor-template.hcl"),
		docsEditorTemplate)
	machine.hostAnswers(map[string]string{"hostname -I": "10.0.0.5"}, nil)

	return machine
}

func TestSync_setupGeneratesTheDocsEditorDefinitionWithTheIDOfTheNode(t *testing.T) {
	t.Parallel()

	machine := docsEditorMachine(t)
	machine.instances["carbonio-docs-editor"] = []Instance{
		{Address: "10.0.0.9", Meta: map[string]string{"service_id": "other"}},
		{Address: "10.0.0.5", Meta: map[string]string{"service_id": "mine"}},
	}

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{
		Services: []string{"carbonio-docs-editor"}, Setup: true, ForceRestart: true,
	}))

	generated, err := os.ReadFile(filepath.Join(machine.env.ServicesDir, "carbonio-docs-editor.hcl"))
	require.NoError(t, err)
	assert.Contains(t, string(generated), `service_id = "mine"`)
	assert.NotContains(t, string(generated), "$METADATA")

	rendered, err := os.ReadFile(filepath.Join(machine.env.RenderedDir, "carbonio-docs-editor.hcl"))
	require.NoError(t, err)
	assert.Contains(t, string(rendered), `token = "secret-1"`)

	require.Len(t, machine.entries, 2)

	resolver, ok := machine.entries[0].(*api.ServiceResolverConfigEntry)
	require.True(t, ok)
	assert.Equal(t, "carbonio-docs-editor", resolver.Name)
	assert.Equal(t, map[string]api.ServiceResolverSubset{
		"mine":  {Filter: `Service.Meta.service_id == "mine"`},
		"other": {Filter: `Service.Meta.service_id == "other"`},
	}, resolver.Subsets)

	router, ok := machine.entries[1].(*api.ServiceRouterConfigEntry)
	require.True(t, ok)
	require.Len(t, router.Routes, 2)
	assert.Equal(t, "mine", router.Routes[0].Destination.ServiceSubset)
	assert.Equal(t, api.ServiceRouteHTTPMatchQueryParam{Name: "service_id", Exact: "mine"},
		router.Routes[0].Match.HTTP.QueryParam[0])
}

func TestSync_docsEditorKeepsItsIDUntilTheCatalogKnowsIt(t *testing.T) {
	t.Parallel()

	machine := docsEditorMachine(t)
	opts := SyncOptions{Services: []string{"carbonio-docs-editor"}, Setup: true}

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))

	first := generatedServiceID(filepath.Join(machine.env.ServicesDir, "carbonio-docs-editor.hcl"))
	require.NotEmpty(t, first, "a new identifier when nothing knows the node")

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))
	assert.Equal(t, first, generatedServiceID(filepath.Join(machine.env.ServicesDir, "carbonio-docs-editor.hcl")))

	router, ok := machine.entries[len(machine.entries)-1].(*api.ServiceRouterConfigEntry)
	require.True(t, ok)
	require.Len(t, router.Routes, 1, "the router has the instance the catalog does not know yet")
	assert.Equal(t, first, router.Routes[0].Destination.ServiceSubset)
}

func TestSync_docsEditorNeedsItsTemplate(t *testing.T) {
	t.Parallel()

	machine := docsEditorMachine(t)
	require.NoError(t, os.Remove(filepath.Join(machine.env.ServicesDir, "templates", "carbonio-docs-editor-template.hcl")))

	err := Sync(machine.env, loadDefs(t), SyncOptions{Services: []string{"carbonio-docs-editor"}, Setup: true})
	require.ErrorContains(t, err, "template")
	assert.Empty(t, machine.entries)
}

func selfSignedCertificate(t *testing.T, path string, names ...string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "cert"},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644))
}

func turnServerMachine(t *testing.T, fqdn string, certNames ...string) *fakeMachine {
	t.Helper()

	machine := newMachine(t)
	machine.users["zextras"] = Account{UID: 1040, GID: 1040}
	machine.serviceDir(t, "turn-server")
	machine.serviceDir(t, "turn-server-auth")
	machine.writeServiceFile(t, filepath.Join(machine.env.CarbonioDir, "turn-server", "turnserver.conf.template"),
		"secret=${TURN_SECRET}\nexternal-ip=${PUBLIC_IP}\nlocal=${LOCAL_IP}\nrealm=${REALM}\n")
	machine.writeServiceFile(t, filepath.Join(machine.env.CertDir, "commercial.key"), "key")
	selfSignedCertificate(t, filepath.Join(machine.env.CertDir, "commercial.crt"), certNames...)

	machine.hostAnswers(map[string]string{
		"hostname -f":                     fqdn,
		"hostname -I":                     "10.0.0.5 10.0.0.6",
		"hostname -d":                     "example.com",
		"debconf-communicate videoserver": "0 203.0.113.7",
	}, func(cmd *Command) ([]byte, error) {
		if cmd.Name == "carbonio-turn-server-configs-hcl" {
			dir := machine.env.ServicesDir
			machine.writeServiceFile(t, filepath.Join(dir, "carbonio-turn-server.hcl"), "services {\n  name = \"carbonio-turn-server\"\n}\n")
			machine.writeServiceFile(t, filepath.Join(dir, "carbonio-turn-server-auth.hcl"), "services {\n  name = \"carbonio-turn-server-auth\"\n}\n")
		}

		return nil, nil
	})

	return machine
}

func TestSync_setupGeneratesTheTurnServerConfigurationOnce(t *testing.T) {
	t.Parallel()

	machine := turnServerMachine(t, "node.example.com", "node.example.com")
	opts := SyncOptions{Services: []string{"carbonio-turn-server", "carbonio-turn-server-auth"}, Setup: true, ForceRestart: true}

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))

	path := filepath.Join(machine.env.CarbonioDir, "turn-server", "turnserver.conf")
	conf, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Regexp(t, `^secret=[0-9a-f]{64}\nexternal-ip=203\.0\.113\.7\nlocal=10\.0\.0\.5\nrealm=example\.com\n$`, string(conf))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	assert.Equal(t, Account{UID: 1040, GID: 1040}, machine.chowns[filepath.Dir(path)])
	assert.Equal(t, Account{UID: 1040, GID: 1040}, machine.chowns[filepath.Join(machine.env.CertDir, "commercial.key")])

	for _, name := range []string{"carbonio-turn-server.hcl", "carbonio-turn-server-auth.hcl"} {
		assert.FileExists(t, filepath.Join(machine.env.RenderedDir, name), "the two definitions produced by one command")
	}

	// A second run leaves the secret alone.
	require.NoError(t, Sync(machine.env, loadDefs(t), opts))

	again, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(conf), string(again))
}

func TestSync_turnServerRefusesAnImproperNode(t *testing.T) {
	t.Parallel()

	opts := SyncOptions{Services: []string{"carbonio-turn-server"}, Setup: true}

	machine := turnServerMachine(t, "localhost", "node.example.com")
	require.ErrorContains(t, Sync(machine.env, loadDefs(t), opts), "hostname is not properly configured")
	assert.NoFileExists(t, filepath.Join(machine.env.CarbonioDir, "turn-server", "turnserver.conf"))

	machine = turnServerMachine(t, "node.example.com", "other.example.com")
	require.ErrorContains(t, Sync(machine.env, loadDefs(t), opts), "does not cover the hostname")
	assert.NoFileExists(t, filepath.Join(machine.env.CarbonioDir, "turn-server", "turnserver.conf"))

	machine = turnServerMachine(t, "node.example.com", "node.example.com")
	require.NoError(t, os.Remove(filepath.Join(machine.env.CertDir, "commercial.crt")))
	require.ErrorContains(t, Sync(machine.env, loadDefs(t), opts), "TLS certificate not found")
}

func TestSync_turnServerPublicAddressDefaultsToTheLocalOne(t *testing.T) {
	t.Parallel()

	machine := turnServerMachine(t, "node.example.com", "*.example.com")
	machine.hostAnswers(map[string]string{
		"hostname -f": "node.example.com", "hostname -I": "10.0.0.5", "hostname -d": "",
	}, nil)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Services: []string{"carbonio-turn-server"}, Setup: true}))

	conf, err := os.ReadFile(filepath.Join(machine.env.CarbonioDir, "turn-server", "turnserver.conf"))
	require.NoError(t, err)
	assert.Contains(t, string(conf), "external-ip=10.0.0.5\n")
	assert.Contains(t, string(conf), "realm=node.example.com\n", "without a domain the realm is the host name")
}

func alertmanagerMachine(t *testing.T, check error) *fakeMachine {
	t.Helper()

	machine := newMachine(t)
	machine.users["carbonio-prometheus"] = Account{UID: 1050, GID: 1050}
	machine.serviceDir(t, "carbonio-prometheus-alertmanager")
	machine.writeServiceFile(t, filepath.Join(machine.env.ServicesDir, "carbonio-prometheus-alertmanager.hcl"),
		"services {\n  name = \"carbonio-prometheus-alertmanager\"\n}\n")
	machine.writeServiceFile(t,
		filepath.Join(machine.env.CarbonioDir, "carbonio-prometheus-alertmanager", "config-templates", "alertmanager.yml"),
		"route:\n  receiver: none\n")
	machine.hostAnswers(nil, func(cmd *Command) ([]byte, error) {
		if cmd.Name == "amtool" {
			// the candidate is what is checked
			assert.Contains(t, cmd.Args[len(cmd.Args)-1], ".candidate")

			return nil, check
		}

		return nil, nil
	})

	return machine
}

func TestSync_setupStoresAndRendersTheAlertmanagerConfiguration(t *testing.T) {
	t.Parallel()

	machine := alertmanagerMachine(t, nil)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{
		Services: []string{"carbonio-prometheus-alertmanager"}, Setup: true, ForceRestart: true,
	}))

	assert.Equal(t, "route:\n  receiver: none\n", machine.kv["carbonio-alertmanager/config"])

	target := filepath.Join(machine.env.CarbonioDir, "carbonio-prometheus-alertmanager", "alertmanager.yml")
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "route:\n  receiver: none\n", string(content))
	assert.NoFileExists(t, target+".candidate")
	assert.Contains(t, machine.restarts(), "carbonio-prometheus-alertmanager.service")
}

func TestSync_invalidAlertmanagerConfigurationDoesNotReplaceTheWorkingOne(t *testing.T) {
	t.Parallel()

	machine := alertmanagerMachine(t, errCommand)
	target := filepath.Join(machine.env.CarbonioDir, "carbonio-prometheus-alertmanager", "alertmanager.yml")
	machine.writeServiceFile(t, target, "working\n")

	err := Sync(machine.env, loadDefs(t), SyncOptions{Services: []string{"carbonio-prometheus-alertmanager"}, Setup: true})
	require.ErrorIs(t, err, errCommand)

	content, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "working\n", string(content))
	assert.NoFileExists(t, target+".candidate")
}

const prometheusConfig = `consul_sd_configs:
  - token: "{{ consultoken }}"
    services: [a.{{ hostsdomain }}, b-{{ consulhostsdomain }}]
`

func prometheusMachine(t *testing.T) (*fakeMachine, string) {
	t.Helper()

	machine := newMachine(t)
	machine.users["carbonio-prometheus"] = Account{UID: 1050, GID: 1050}
	machine.writeServiceFile(t, filepath.Join(machine.env.ServicesDir, "carbonio-prometheus.hcl"),
		"services {\n  name = \"carbonio-prometheus\"\n}\n")
	machine.hostAnswers(map[string]string{"hostname -d": "example.com"}, nil)

	path := filepath.Join(machine.env.CarbonioDir, "carbonio-prometheus", "prometheus.yml")
	machine.writeServiceFile(t, path, prometheusConfig)

	return machine, path
}

func TestSync_prometheusConfigurationFollowsTheToken(t *testing.T) {
	t.Parallel()

	machine, path := prometheusMachine(t)
	opts := SyncOptions{Services: []string{"carbonio-prometheus"}}

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "consul_sd_configs:\n  - token: \"secret-1\"\n    services: [a.example.com, b-example-com]\n", string(content))
	assert.Contains(t, machine.restarts(), "carbonio-prometheus.service")

	// Nothing to do the second time.
	machine.systemctl = nil

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))
	assert.Empty(t, machine.restarts())

	// A new token replaces the one in the file.
	opts.Renew = true

	require.NoError(t, Sync(machine.env, loadDefs(t), opts))

	content, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), `token: "secret-2"`)
	assert.NotContains(t, string(content), "secret-1")
	assert.Contains(t, machine.restarts(), "carbonio-prometheus.service")
}

func TestSync_prometheusRegistersItself(t *testing.T) {
	t.Parallel()

	prometheus, found := Find(loadDefs(t), "carbonio-prometheus")
	require.True(t, found)

	rules, err := prometheus.ExtraPolicy()
	require.NoError(t, err)
	assert.Contains(t, rules, `"carbonio-prometheus"`)
	assert.Contains(t, rules, "write")
	assert.False(t, prometheus.ConsumerOnly)
}

func TestExporterDefinitions(t *testing.T) {
	t.Parallel()

	defs := loadDefs(t)

	var exporters int

	for i := range defs {
		def := &defs[i]
		if !strings.HasPrefix(def.Name, "carbonio-prometheus-") {
			continue
		}

		exporters++

		assert.Equal(t, "carbonio-prometheus", def.User, def.Name, "the user the packages share")
		assert.Equal(t, []string{def.Name + ".hcl"}, def.HCL, def.Name)
		assert.Equal(t, def.Name, def.Dir, def.Name)

		policy, err := def.Policy()
		require.NoError(t, err, def.Name)
		assert.Contains(t, policy, `"`+def.Name+`"`, "it registers its own service")
		assert.NotContains(t, policy, "service_prefix", "and no other")
	}

	assert.Equal(t, 2, exporters, "alertmanager and the OpenLDAP exporter, the ones of the fixture")
}
