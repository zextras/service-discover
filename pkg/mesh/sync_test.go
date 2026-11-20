// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	filesHCL = `services {
  name = "carbonio-files"
  port = 10000
}
`
	fetchFailures = 2
)

// fakeMachine implements the Env of a node, with the directories in a temporary directory.
type fakeMachine struct {
	env        *Env
	users      map[string]Account
	chowns     map[string]Account
	fetched    []string // SPIFFE IDs requested, with the uid they were requested as
	logins     int
	tokens     map[string]bool // valid tokens
	loggedOut  []string
	systemctl  [][]string
	inactive   bool // Consul is stopped: it is not active and has no process to signal
	starting   bool // Consul is activating (ExecStartPost): not active, but running
	failFetch  int  // number of fetches failing before one works
	failUnit   string
	now        time.Time
	slept      time.Duration
	nextSecret int

	kv        map[string]string // the KV store, whatever the token
	kvTokens  []string          // tokens the KV store was used with
	instances map[string][]Instance
	entries   []api.ConfigEntry
	commands  []*Command
	exec      func(*Command) ([]byte, error) // answers the commands, nothing is run
}

// fakeConsul is the KV store and the catalog of the machine.
type fakeConsul struct{ machine *fakeMachine }

func (c fakeConsul) Get(key string) (string, bool, error) {
	value, found := c.machine.kv[key]

	return value, found, nil
}

func (c fakeConsul) Put(key, value string) error {
	c.machine.kv[key] = value

	return nil
}

func (c fakeConsul) Delete(key string) error {
	delete(c.machine.kv, key)

	return nil
}

func (c fakeConsul) Instances(service string, _ bool) ([]Instance, error) {
	return c.machine.instances[service], nil
}

func (c fakeConsul) SetConfigEntry(entry api.ConfigEntry) error {
	c.machine.entries = append(c.machine.entries, entry)

	return nil
}

var (
	errFetch = errors.New("no identity yet")
	errUnit  = errors.New("unit failed")
)

func newMachine(t *testing.T) *fakeMachine {
	t.Helper()

	root := t.TempDir()
	machine := &fakeMachine{
		users: map[string]Account{
			ConsulUser:         {UID: 900, GID: 900},
			"carbonio-files":   {UID: 1001, GID: 1001},
			"carbonio-auth":    {UID: 1002, GID: 1002},
			"carbonio-mailbox": {UID: 1003, GID: 1003},
		},
		chowns:    map[string]Account{},
		tokens:    map[string]bool{},
		now:       time.Unix(0, 0),
		kv:        map[string]string{},
		instances: map[string][]Instance{},
	}

	machine.env = &Env{
		ServicesDir: filepath.Join(root, "service-discover"),
		RenderedDir: filepath.Join(root, "rendered"),
		CarbonioDir: filepath.Join(root, "carbonio"),
		SystemdDir:  filepath.Join(root, "systemd"),
		TrustDomain: "carbonio.lab",
		LookupUser: func(name string) (Account, error) {
			account, found := machine.users[name]
			if !found {
				return Account{}, fs.ErrNotExist
			}

			return account, nil
		},
		Chown: func(path string, uid, gid int) error {
			// files are written to a temporary name next to their destination: the owner is recorded by directory.
			if strings.HasPrefix(filepath.Base(path), ".mesh-") {
				path = filepath.Dir(path)
			}

			machine.chowns[path] = Account{UID: uid, GID: gid}

			return nil
		},
		FetchJWT: func(account Account, spiffeID string) (string, error) {
			machine.fetched = append(machine.fetched, fmt.Sprintf("%d %s", account.UID, spiffeID))

			if machine.failFetch > 0 {
				machine.failFetch--

				return "", errFetch
			}

			return "jwt-for-" + spiffeID, nil
		},
		Login: func(string) (string, error) {
			machine.logins++
			machine.nextSecret++
			secret := fmt.Sprintf("secret-%d", machine.nextSecret)
			machine.tokens[secret] = true

			return secret, nil
		},
		Valid: func(token string) (bool, error) { return machine.tokens[token], nil },
		Logout: func(token string) error {
			machine.loggedOut = append(machine.loggedOut, token)
			delete(machine.tokens, token)

			return nil
		},
		Systemctl: func(args ...string) error {
			machine.systemctl = append(machine.systemctl, args)

			if args[0] == "is-active" && (machine.inactive || machine.starting) {
				return errUnit
			}

			if args[0] == "kill" && machine.inactive {
				return errUnit
			}

			if args[0] == "restart" && args[len(args)-1] == machine.failUnit {
				return errUnit
			}

			return nil
		},
		Sleep: func(d time.Duration) {
			machine.slept += d
			machine.now = machine.now.Add(d)
		},
		Now:     func() time.Time { return machine.now },
		CertDir: filepath.Join(root, "ssl"),
		Exec: func(cmd *Command) ([]byte, error) {
			machine.commands = append(machine.commands, cmd)

			if machine.exec == nil {
				return nil, nil
			}

			return machine.exec(cmd)
		},
		Consul: func(token string) (Consul, error) {
			machine.kvTokens = append(machine.kvTokens, token)

			return fakeConsul{machine: machine}, nil
		},
	}

	require.NoError(t, os.MkdirAll(machine.env.ServicesDir, 0o755))

	return machine
}

func (m *fakeMachine) installFiles(t *testing.T) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(m.env.ServicesDir, "carbonio-files.hcl"), []byte(filesHCL), 0o644))
}

func (m *fakeMachine) tokenFile(dir string) string {
	return filepath.Join(m.env.CarbonioDir, dir, "service-discover", "token")
}

func (m *fakeMachine) restarts() []string {
	var units []string

	for _, call := range m.systemctl {
		if call[0] == "restart" {
			units = append(units, call[len(call)-1])
		}
	}

	slices.Sort(units)

	return units
}

func (m *fakeMachine) calls(verb string) int {
	count := 0

	for _, call := range m.systemctl {
		if call[0] == verb || (call[0] == "kill" && verb == "reload") {
			count++
		}
	}

	return count
}

func loadDefs(t *testing.T) []Definition {
	t.Helper()

	defs, err := Load()
	require.NoError(t, err)

	return defs
}

func TestSync_issuesTheTokenAsTheServiceUser(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	out := &bytes.Buffer{}
	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Out: out}))

	assert.Equal(t, []string{"1001 spiffe://carbonio.lab/service/carbonio-files"}, machine.fetched,
		"the identity is requested with the uid of the service")

	tokenPath := machine.tokenFile("files")
	token, err := os.ReadFile(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "secret-1", string(token))

	info, err := os.Stat(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Equal(t, Account{UID: 1001, GID: 1001}, machine.chowns[filepath.Dir(tokenPath)])

	rendered := filepath.Join(machine.env.RenderedDir, "carbonio-files.hcl")
	content, err := os.ReadFile(rendered)
	require.NoError(t, err)
	assert.Contains(t, string(content), `token = "secret-1"`)
	assert.Equal(t, Account{UID: 900, GID: 900}, machine.chowns[machine.env.RenderedDir], "Consul reads the rendered files")

	info, err = os.Stat(rendered)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	dropIn, err := os.ReadFile(filepath.Join(machine.env.SystemdDir, "carbonio-files.service.d", "override.conf"))
	require.NoError(t, err)
	assert.Equal(t, "[Service]\nEnvironment=\"CONSUL_HTTP_TOKEN=secret-1\"\n", string(dropIn))

	assert.Equal(t, []string{"carbonio-files-sidecar.service", "carbonio-files-watches.service", "carbonio-files.service"},
		machine.restarts())
	assert.Equal(t, 1, machine.calls("reload"))
	assert.Equal(t, 1, machine.calls("daemon-reload"))
	assert.Contains(t, out.String(), "carbonio-files: new token written")
}

func TestSync_onlyInstalledServices(t *testing.T) {
	t.Parallel()

	machine := newMachine(t) // no package definition on the node
	machine.users["carbonio-docs-connector"] = Account{UID: 5, GID: 5}

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))
	assert.Empty(t, machine.fetched)
	assert.Empty(t, machine.restarts())

	// The mailbox services have no definition of their own to render: their service directory marks the install.
	require.NoError(t, os.MkdirAll(filepath.Join(machine.env.CarbonioDir, "mailbox", "service-discover"), 0o755))

	defs := loadDefs(t)
	installed := machine.env.Installed(defs)

	var names []string
	for _, def := range installed {
		names = append(names, def.Name)
	}

	assert.NotContains(t, names, "carbonio-files")
}

func TestSync_isIdempotent(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	machine.systemctl = nil
	machine.fetched = nil

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	assert.Equal(t, 1, machine.logins, "a valid token is kept")
	assert.Empty(t, machine.fetched)
	assert.Empty(t, machine.restarts(), "nothing changed, nothing restarts")
	assert.Equal(t, 0, machine.calls("reload"))
}

func TestSync_replacesAnInvalidToken(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	delete(machine.tokens, "secret-1") // destroyed in Consul

	machine.systemctl = nil

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	token, err := os.ReadFile(machine.tokenFile("files"))
	require.NoError(t, err)
	assert.Equal(t, "secret-2", string(token))
	assert.Empty(t, machine.loggedOut, "a token that is gone does not need a logout")
	assert.Contains(t, machine.restarts(), "carbonio-files-sidecar.service")
}

func TestSync_renewLogsOutTheSupersededToken(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))
	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Renew: true}))

	token, err := os.ReadFile(machine.tokenFile("files"))
	require.NoError(t, err)
	assert.Equal(t, "secret-2", string(token))
	assert.Equal(t, []string{"secret-1"}, machine.loggedOut)
	assert.False(t, machine.tokens["secret-1"])
	assert.True(t, machine.tokens["secret-2"])

	content, err := os.ReadFile(filepath.Join(machine.env.RenderedDir, "carbonio-files.hcl"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "secret-2")
	assert.NotContains(t, string(content), "secret-1")
}

func TestSync_waitsForTheIdentity(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	machine.failFetch = fetchFailures

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Wait: time.Minute}))
	assert.Len(t, machine.fetched, fetchFailures+1)
	assert.Equal(t, fetchFailures*fetchInterval, machine.slept)
}

func TestSync_givesUpAfterTheWait(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	machine.failFetch = 1000

	err := Sync(machine.env, loadDefs(t), SyncOptions{Wait: 5 * time.Second})
	require.ErrorIs(t, err, errFetch)
	require.ErrorContains(t, err, "carbonio-files")
	assert.NoFileExists(t, machine.tokenFile("files"))
	assert.Empty(t, machine.restarts())
}

func TestSync_oneFailureDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	require.NoError(t, os.WriteFile(filepath.Join(machine.env.ServicesDir, "carbonio-auth.hcl"),
		[]byte("service = \"broken\""), 0o644))

	err := Sync(machine.env, loadDefs(t), SyncOptions{})
	require.ErrorIs(t, err, errUnsupportedHCL)
	require.ErrorContains(t, err, "carbonio-auth")

	assert.FileExists(t, filepath.Join(machine.env.RenderedDir, "carbonio-files.hcl"))
	assert.Contains(t, machine.restarts(), "carbonio-files.service")
}

func TestSync_restartFailuresAreReported(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	machine.failUnit = "carbonio-files.service"

	err := Sync(machine.env, loadDefs(t), SyncOptions{})
	require.ErrorIs(t, err, errUnit)
	require.ErrorContains(t, err, "carbonio-files.service")
	assert.Contains(t, machine.restarts(), "carbonio-files-sidecar.service", "the other units are restarted anyway")
}

func TestSync_noRestart(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{NoRestart: true}))

	assert.FileExists(t, machine.tokenFile("files"))
	assert.Empty(t, machine.systemctl)
}

func TestSync_noBlock(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{NoBlock: true}))

	assert.Contains(t, machine.systemctl, []string{"restart", "--no-block", "carbonio-files.service"})
}

func TestSync_ignoresAStoppedConsul(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	machine.inactive = true

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	assert.FileExists(t, filepath.Join(machine.env.RenderedDir, "carbonio-files.hcl"), "read when Consul starts")
}

// The sync run by service-discover.service after its start finds the unit activating: the definitions it renders
// have to be loaded by the running agent.
func TestSync_signalsAConsulStartingUp(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	machine.starting = true

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	assert.Equal(t, 1, machine.calls("reload"))
	assert.Contains(t, machine.systemctl, []string{"kill", "--signal=SIGHUP", "--kill-whom=main", ConsulUnit})
}

func TestSync_removesTheDefinitionOfARemovedPackage(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))
	require.NoError(t, os.Remove(filepath.Join(machine.env.ServicesDir, "carbonio-files.hcl")))

	machine.systemctl = nil

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	assert.NoFileExists(t, filepath.Join(machine.env.RenderedDir, "carbonio-files.hcl"))
	assert.Equal(t, 1, machine.calls("reload"))
}

func TestSync_selectedServices(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	err := Sync(machine.env, loadDefs(t), SyncOptions{Services: []string{"carbonio-nope"}})
	require.ErrorIs(t, err, errUnknownService)

	err = Sync(machine.env, loadDefs(t), SyncOptions{Services: []string{"carbonio-auth"}})
	require.ErrorIs(t, err, errUnknownService)
	require.ErrorContains(t, err, "not installed")

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{Services: []string{"carbonio-files"}}))
	assert.FileExists(t, machine.tokenFile("files"))
}

func TestSync_serviceUserMustExist(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	delete(machine.users, "carbonio-files")

	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))
	assert.Empty(t, machine.fetched, "a service whose user does not exist is not installed")
}

func TestConsulConfigArgs(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	for name, content := range map[string]string{
		"main.json":              `{}`,
		"config.json":            `{}`,
		"carbonio-unknown.hcl":   "service {\n name = \"x\"\n}\n",
		"README":                 "not a config file",
		".hidden.json":           `{}`,
		"carbonio-files.hcl.rpm": "x",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(machine.env.ServicesDir, name), []byte(content), 0o644))
	}

	require.NoError(t, os.MkdirAll(filepath.Join(machine.env.ServicesDir, "templates"), 0o755))

	defs := loadDefs(t)

	// Without a token the service is not registered.
	args, err := ConsulConfigArgs(machine.env, defs)
	require.NoError(t, err)

	assert.Equal(t, []string{"-config-dir", machine.env.RenderedDir}, args)

	for _, name := range []string{"carbonio-unknown.hcl", "config.json", "main.json"} {
		copied, readErr := os.ReadFile(filepath.Join(machine.env.RenderedDir, name))
		require.NoError(t, readErr, name)

		original, readErr := os.ReadFile(filepath.Join(machine.env.ServicesDir, name))
		require.NoError(t, readErr)
		assert.Equal(t, original, copied, "the files that are not service definitions of a known service are copied")
	}

	for _, name := range []string{"README", ".hidden.json", "carbonio-files.hcl.rpm", "templates"} {
		assert.NoFileExists(t, filepath.Join(machine.env.RenderedDir, name))
	}

	assert.NoFileExists(t, filepath.Join(machine.env.RenderedDir, "carbonio-files.hcl"))
	assert.Empty(t, machine.fetched, "the preparation does not talk to SPIRE")

	// Once the token exists the definition is rendered, offline.
	require.NoError(t, os.MkdirAll(filepath.Dir(machine.tokenFile("files")), 0o755))
	require.NoError(t, os.WriteFile(machine.tokenFile("files"), []byte("persisted\n"), 0o600))

	args, err = ConsulConfigArgs(machine.env, defs)
	require.NoError(t, err)
	assert.Equal(t, []string{"-config-dir", machine.env.RenderedDir}, args)

	content, err := os.ReadFile(filepath.Join(machine.env.RenderedDir, "carbonio-files.hcl"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `token = "persisted"`)
	assert.Zero(t, machine.logins)
	assert.Equal(t, Account{UID: 900, GID: 900}, machine.chowns[machine.env.RenderedDir])

	// A package removed after the last synchronization is dropped from the rendered definitions.
	require.NoError(t, os.Remove(filepath.Join(machine.env.ServicesDir, "carbonio-files.hcl")))

	_, err = ConsulConfigArgs(machine.env, defs)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(machine.env.RenderedDir, "carbonio-files.hcl"))

	// So is the copy of a file that is gone.
	require.NoError(t, os.Remove(filepath.Join(machine.env.ServicesDir, "main.json")))

	_, err = ConsulConfigArgs(machine.env, defs)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(machine.env.RenderedDir, "main.json"))
}

func TestSync_lateInstalledDefinitionsReloadConsulWithoutARestart(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	defs := loadDefs(t)
	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))

	reloads := machine.calls("reload")

	// A service of a package nobody knows is installed while Consul runs: it is copied and Consul reloaded.
	require.NoError(t, os.WriteFile(filepath.Join(machine.env.ServicesDir, "carbonio-late.hcl"), []byte("service {}\n"), 0o644))
	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))

	assert.FileExists(t, filepath.Join(machine.env.RenderedDir, "carbonio-late.hcl"))
	assert.Equal(t, reloads+1, machine.calls("reload"))
	assert.NotContains(t, machine.restarts(), "service-discover.service", "Consul is reloaded, never restarted")

	// Nothing changed: no reload.
	require.NoError(t, Sync(machine.env, defs, SyncOptions{}))
	assert.Equal(t, reloads+1, machine.calls("reload"))
}

func TestConsulConfigArgs_brokenDefinitionIsLeftOut(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	require.NoError(t, os.WriteFile(filepath.Join(machine.env.ServicesDir, "carbonio-files.hcl"), []byte("service = 3"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Dir(machine.tokenFile("files")), 0o755))
	require.NoError(t, os.WriteFile(machine.tokenFile("files"), []byte("persisted"), 0o600))

	args, err := ConsulConfigArgs(machine.env, loadDefs(t))
	require.ErrorIs(t, err, errUnsupportedHCL)
	assert.True(t, strings.HasSuffix(strings.Join(args, " "), "-config-dir "+machine.env.RenderedDir),
		"Consul can start anyway")
}

func TestSync_changesBeforeAFailureStillRestartTheUnits(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	// the drop-in cannot be written: the token and the definition are, and the next run would find them up to date
	require.NoError(t, os.WriteFile(machine.env.SystemdDir, []byte("not a directory"), 0o600))

	err := Sync(machine.env, loadDefs(t), SyncOptions{})
	require.Error(t, err)

	assert.Contains(t, machine.restarts(), "carbonio-files-sidecar.service")
	assert.Equal(t, 1, machine.calls("reload"))
}
