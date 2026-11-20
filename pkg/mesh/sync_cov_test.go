// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errCov = errors.New("injected failure")

// covConsul is a Consul whose answers, and failures, the test chooses.
type covConsul struct {
	kv           map[string]string
	stale        string // answer of Get when set, whatever the key
	instances    []Instance
	getErr       error
	putErr       error
	deleteErr    error
	instancesErr error
	entryFail    map[string]error // by kind of config entry
	entries      []api.ConfigEntry
}

func (c *covConsul) Get(key string) (string, bool, error) {
	if c.getErr != nil {
		return "", false, c.getErr
	}

	if c.stale != "" {
		return c.stale, true, nil
	}

	value, found := c.kv[key]

	return value, found, nil
}

func (c *covConsul) Put(key, value string) error {
	if c.putErr != nil {
		return c.putErr
	}

	if c.kv == nil {
		c.kv = map[string]string{}
	}

	c.kv[key] = value

	return nil
}

func (c *covConsul) Delete(key string) error {
	if c.deleteErr != nil {
		return c.deleteErr
	}

	delete(c.kv, key)

	return nil
}

func (c *covConsul) Instances(string, bool) ([]Instance, error) {
	return c.instances, c.instancesErr
}

func (c *covConsul) SetConfigEntry(entry api.ConfigEntry) error {
	err := c.entryFail[entry.GetKind()]
	if err != nil {
		return err
	}

	c.entries = append(c.entries, entry)

	return nil
}

// useConsul makes the machine talk to the given Consul.
func (m *fakeMachine) useConsul(consul Consul) {
	m.env.Consul = func(string) (Consul, error) { return consul, nil }
}

// covLogger records the lines logged.
type covLogger struct{ lines []string }

func (l *covLogger) log(format string, args ...any) {
	l.lines = append(l.lines, strings.TrimSpace(format+" "+strings.Join(nilSafe(args), " ")))
}

func nilSafe(args []any) []string {
	out := make([]string, 0, len(args))

	for _, arg := range args {
		if s, ok := arg.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

func covFindDef(t *testing.T, name string) *Definition {
	t.Helper()

	def, found := Find(loadDefs(t), name)
	require.True(t, found, name)

	return def
}

func noLog(string, ...any) {}

// failChown makes the machine unable to change the owner of the paths the predicate selects.
func (m *fakeMachine) failChown(selects func(path string) bool) {
	orig := m.env.Chown
	m.env.Chown = func(path string, uid, gid int) error {
		if selects(path) {
			return errCov
		}

		return orig(path, uid, gid)
	}
}

func isTemporary(path string) bool { return strings.HasPrefix(filepath.Base(path), ".mesh-") }

// blocker creates a file, so that what must be created below it fails.
func covBlocker(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	return path
}

func skipAsRoot(t *testing.T) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("permissions do not apply to root")
	}
}

func TestCovSync_failuresOfTheSharedDirectories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		corrupt func(t *testing.T, m *fakeMachine)
		want    string
	}{
		{"services directory missing", func(t *testing.T, m *fakeMachine) {
			t.Helper()
			require.NoError(t, os.RemoveAll(m.env.ServicesDir))
		}, "unable to read the configuration of the agent"},
		{"rendered directory is a file", func(t *testing.T, m *fakeMachine) {
			t.Helper()
			require.NoError(t, os.WriteFile(m.env.RenderedDir, nil, 0o644))
		}, "not a directory"},
		{"no Consul user", func(t *testing.T, m *fakeMachine) {
			t.Helper()
			m.installFiles(t)
			delete(m.users, ConsulUser)
		}, "unable to find the Consul user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			tt.corrupt(t, machine)

			err := Sync(machine.env, loadDefs(t), SyncOptions{})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCovSync_markerThatCannotBeRemovedIsReported(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)

	marker := machine.env.sidecarsMarker()
	require.NoError(t, os.WriteFile(marker, nil, 0o644))

	orig := machine.env.Systemctl
	swapped := false
	machine.env.Systemctl = func(args ...string) error {
		if !swapped {
			// The marker becomes a directory that is not empty: it cannot be removed.
			swapped = true

			require.NoError(t, os.Remove(marker))
			require.NoError(t, os.MkdirAll(filepath.Join(marker, "child"), 0o755))
		}

		return orig(args...)
	}

	err := Sync(machine.env, loadDefs(t), SyncOptions{})
	require.ErrorContains(t, err, "restart-sidecars")
}

func TestCovSync_systemctlFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		failing string
		want    string
	}{
		{"daemon-reload", "daemon-reload", "systemctl daemon-reload"},
		{"reload of a running agent", "kill", "unable to reload"},
		{"unit that is not installed", "cat", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			machine.installFiles(t)

			orig := machine.env.Systemctl
			machine.env.Systemctl = func(args ...string) error {
				if args[0] == tt.failing {
					machine.systemctl = append(machine.systemctl, args)

					return errUnit
				}

				return orig(args...)
			}

			err := Sync(machine.env, loadDefs(t), SyncOptions{})
			if tt.want == "" {
				require.NoError(t, err)
				assert.Empty(t, machine.restarts(), "a unit the package does not ship is not restarted")

				return
			}

			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCovSync_logoutFailureIsReported(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.installFiles(t)
	require.NoError(t, Sync(machine.env, loadDefs(t), SyncOptions{}))

	machine.env.Logout = func(string) error { return errCov }

	err := Sync(machine.env, loadDefs(t), SyncOptions{Renew: true})
	require.ErrorIs(t, err, errCov)
	require.ErrorContains(t, err, "unable to log out the previous token")
}

func TestCovSync_tokenFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(m *fakeMachine)
		want    string
	}{
		{"login", func(m *fakeMachine) {
			m.env.Login = func(string) (string, error) { return "", errCov }
		}, "unable to log in Consul"},
		{"token file", func(m *fakeMachine) {
			m.failChown(isTemporary)
		}, errCov.Error()},
		{"inspection of the current token", func(m *fakeMachine) {
			m.tokens["current"] = true
			m.env.IsLogin = func(string) (bool, error) { return false, errCov }
		}, "unable to inspect the token"},
		{"validation of the current token", func(m *fakeMachine) {
			m.env.Valid = func(string) (bool, error) { return false, errCov }
		}, "unable to validate the token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			machine.installFiles(t)
			require.NoError(t, os.MkdirAll(filepath.Dir(machine.tokenFile("files")), 0o755))
			require.NoError(t, os.WriteFile(machine.tokenFile("files"), []byte("current"), 0o640))

			tt.prepare(machine)

			err := Sync(machine.env, loadDefs(t), SyncOptions{})
			require.ErrorContains(t, err, tt.want)
			require.ErrorIs(t, err, errCov)
		})
	}
}

func TestCovSync_validTokenWaitsWhileConsulFails(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.tokens["t"] = true
	machine.failValid = 2

	valid, err := validToken(machine.env, "t", machine.now.Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, valid)
	assert.Equal(t, 2*fetchInterval, machine.slept)
}

func TestCovSync_syncServiceFailures(t *testing.T) {
	t.Parallel()

	t.Run("unknown user", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		def := covFindDef(t, "carbonio-files")
		delete(machine.users, def.User)

		_, err := syncService(machine.env, def, SyncOptions{}, machine.now, noLog)
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("placeholders that cannot be read", func(t *testing.T) {
		t.Parallel()

		machine, path := prometheusMachine(t)
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.MkdirAll(path, 0o755))

		def := covFindDef(t, "carbonio-prometheus")

		res, err := syncService(machine.env, def, SyncOptions{}, machine.now, noLog)
		require.Error(t, err)
		assert.Equal(t, "secret-1", res.token, "what was done before the failure is kept")
	})
}

func TestCovSync_finishSetupReportsTheServicesThatFail(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.env.Consul = func(string) (Consul, error) { return nil, errCov }

	docs := covFindDef(t, "carbonio-docs-editor")
	files := covFindDef(t, "carbonio-files")

	failures := finishSetup(machine.env, []outcome{
		{def: docs, token: "t"},
		{def: files, token: ""}, // not set up: not routed
	}, SyncOptions{}, noLog)

	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0], errCov)
	require.ErrorContains(t, failures[0], "carbonio-docs-editor")
}

func TestCovSync_removeStale(t *testing.T) {
	t.Parallel()

	t.Run("directories are left alone", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		require.NoError(t, os.MkdirAll(filepath.Join(machine.env.RenderedDir, "subdir"), 0o755))

		removed, err := removeStale(machine.env, loadDefs(t), noLog)
		require.NoError(t, err)
		assert.Empty(t, removed)
		assert.DirExists(t, filepath.Join(machine.env.RenderedDir, "subdir"))
	})

	t.Run("a copy that cannot be removed", func(t *testing.T) {
		t.Parallel()
		skipAsRoot(t)

		machine := newMachine(t)
		require.NoError(t, os.MkdirAll(machine.env.RenderedDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(machine.env.RenderedDir, "stale.hcl"), nil, 0o644))
		require.NoError(t, os.Chmod(machine.env.RenderedDir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(machine.env.RenderedDir, 0o755) })

		_, err := removeStale(machine.env, loadDefs(t), noLog)
		require.ErrorIs(t, err, fs.ErrPermission)
	})
}

// covBreakers make the writing of a file under a directory fail, each in its own way.
var covBreakers = []struct {
	name    string
	corrupt func(t *testing.T, m *fakeMachine)
	want    string
}{
	{"the directory cannot be created", func(t *testing.T, m *fakeMachine) {
		t.Helper()
		m.env.RenderedDir = filepath.Join(covBlocker(t), "sub")
	}, "not a directory"},
	{"the directory cannot be given to Consul", func(t *testing.T, m *fakeMachine) {
		t.Helper()
		m.failChown(func(path string) bool { return path == m.env.RenderedDir })
	}, errCov.Error()},
	{"the file cannot be written", func(t *testing.T, m *fakeMachine) {
		t.Helper()
		m.failChown(isTemporary)
	}, errCov.Error()},
}

func TestCovSync_renderDefinitionsFailures(t *testing.T) {
	t.Parallel()

	for _, tt := range covBreakers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			machine.installFiles(t)
			tt.corrupt(t, machine)

			changed, err := renderDefinitions(machine.env, covFindDef(t, "carbonio-files"), "tok", noLog)
			require.ErrorContains(t, err, tt.want)
			assert.False(t, changed)
		})
	}

	t.Run("no Consul user", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		delete(machine.users, ConsulUser)

		_, err := renderDefinitions(machine.env, covFindDef(t, "carbonio-files"), "tok", noLog)
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.ErrorContains(t, err, "unable to find the Consul user")
	})

	t.Run("definition that cannot be read", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		require.NoError(t, os.MkdirAll(filepath.Join(machine.env.ServicesDir, "carbonio-files.hcl"), 0o755))

		_, err := renderDefinitions(machine.env, covFindDef(t, "carbonio-files"), "tok", noLog)
		require.Error(t, err)
	})
}

func TestCovSync_mirrorPassThroughFailures(t *testing.T) {
	t.Parallel()

	for _, tt := range covBreakers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			require.NoError(t, os.WriteFile(filepath.Join(machine.env.ServicesDir, "agent.json"), []byte("{}"), 0o644))
			tt.corrupt(t, machine)

			changed, err := mirrorPassThrough(machine.env, loadDefs(t), noLog)
			require.ErrorContains(t, err, tt.want)
			assert.False(t, changed)
		})
	}

	t.Run("file that cannot be read", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		require.NoError(t, os.Symlink(filepath.Join(machine.env.ServicesDir, "missing"),
			filepath.Join(machine.env.ServicesDir, "agent.json")))

		_, err := mirrorPassThrough(machine.env, loadDefs(t), noLog)
		require.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestCovSync_writeDropInFailures(t *testing.T) {
	t.Parallel()

	tokenEnv := TokenEnv{Unit: "unit.service", Var: "CONSUL_HTTP_TOKEN"}

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		machine.env.SystemdDir = filepath.Join(covBlocker(t), "sub")

		_, err := writeDropIn(machine.env, tokenEnv, "tok", noLog)
		require.Error(t, err)
	})

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		machine.failChown(isTemporary)

		_, err := writeDropIn(machine.env, tokenEnv, "tok", noLog)
		require.ErrorIs(t, err, errCov)
	})
}

func TestCovSync_writeFileFailures(t *testing.T) {
	t.Parallel()

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		require.Error(t, writeFile(machine.env, filepath.Join(covBlocker(t), "sub", "f"), nil, 0o600, Account{}))
	})

	t.Run("temporary file", func(t *testing.T) {
		t.Parallel()
		skipAsRoot(t)

		machine := newMachine(t)
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		require.ErrorIs(t, writeFile(machine.env, filepath.Join(dir, "f"), nil, 0o600, Account{}), fs.ErrPermission)
	})

	t.Run("owner", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		machine.failChown(isTemporary)

		require.ErrorIs(t, writeFile(machine.env, filepath.Join(t.TempDir(), "f"), nil, 0o600, Account{}), errCov)
	})

	t.Run("replacement of a directory", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		target := filepath.Join(t.TempDir(), "target")
		require.NoError(t, os.MkdirAll(filepath.Join(target, "child"), 0o755))

		require.Error(t, writeFile(machine.env, target, nil, 0o600, Account{}))
	})
}

func TestCovSetup_prepareServiceFailures(t *testing.T) {
	t.Parallel()

	t.Run("migration", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		machine.env.Consul = func(string) (Consul, error) { return nil, errCov }

		def := &Definition{Name: "svc", Migrations: Migrations{KV: []KVMove{{Old: "a", New: "b"}}}}

		err := prepareService(&setupContext{env: machine.env, def: def, log: noLog})
		require.ErrorIs(t, err, errCov)
		require.ErrorContains(t, err, "migration")
	})

	t.Run("step", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		def := &Definition{Name: "svc", User: "ghost", Setup: "turn-server"}

		err := prepareService(&setupContext{env: machine.env, def: def, log: noLog})
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.ErrorContains(t, err, "turn-server")
	})
}

func TestCovSetup_finishServiceStepFailure(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.env.Consul = func(string) (Consul, error) { return nil, errCov }

	err := finishService(&setupContext{env: machine.env, def: covFindDef(t, "carbonio-docs-editor"), log: noLog}, 0)
	require.ErrorIs(t, err, errCov)
	require.ErrorContains(t, err, "docs-editor: ")
}

func TestCovSetup_waitLocalInstance(t *testing.T) {
	t.Parallel()

	local := Instance{Address: "10.0.0.5"}
	other := Instance{Address: "10.0.0.9"}

	tests := []struct {
		name      string
		consul    *covConsul
		noConsul  bool
		wantSlept time.Duration
		wantLog   bool
	}{
		{name: "no access to Consul", noConsul: true},
		{name: "healthy at once", consul: &covConsul{instances: []Instance{other, local}}},
		{name: "only another node is healthy", consul: &covConsul{instances: []Instance{other}},
			wantSlept: 4 * time.Second, wantLog: true},
		{name: "catalog failing", consul: &covConsul{instancesErr: errCov},
			wantSlept: 4 * time.Second, wantLog: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			machine.hostAnswers(map[string]string{"hostname -I": "10.0.0.5"}, nil)

			if tt.noConsul {
				machine.env.Consul = func(string) (Consul, error) { return nil, errCov }
			} else {
				machine.useConsul(tt.consul)
			}

			logger := &covLogger{}
			ctx := &setupContext{env: machine.env, def: &Definition{Name: "svc"}, log: logger.log}

			waitLocalInstance(ctx, 3*time.Second)

			assert.Equal(t, tt.wantSlept, machine.slept)
			assert.Equal(t, tt.wantLog, len(logger.lines) == 1, logger.lines)
		})
	}
}

func TestCovSetup_localAddresses(t *testing.T) {
	t.Parallel()

	t.Run("from hostname", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		machine.hostAnswers(map[string]string{"hostname -I": "10.0.0.5 fd00::1"}, nil)

		assert.Equal(t, map[string]bool{"10.0.0.5": true, "fd00::1": true}, localAddresses(machine.env))
		assert.Equal(t, "10.0.0.5", localIP(machine.env))
	})

	t.Run("from the interfaces when hostname fails", func(t *testing.T) {
		t.Parallel()

		machine := newMachine(t)
		machine.exec = func(*Command) ([]byte, error) { return nil, errCommand }

		want := map[string]bool{}

		list, err := net.InterfaceAddrs()
		require.NoError(t, err)

		for _, address := range list {
			ip, _, parseErr := net.ParseCIDR(address.String())
			if parseErr == nil && !ip.IsLoopback() {
				want[ip.String()] = true
			}
		}

		assert.Equal(t, want, localAddresses(machine.env))
		assert.Empty(t, localIP(machine.env))
	})
}

func TestCovSetup_runCommandsSkipsBlankLines(t *testing.T) {
	t.Parallel()

	machine := newMachine(t)
	machine.hostAnswers(map[string]string{"hostname -I": "10.0.0.5"}, nil)

	ctx := &setupContext{env: machine.env, def: &Definition{Name: "svc"}, token: "tok", log: noLog}
	require.NoError(t, runCommands(ctx, []string{"", "  \t", "tool --flag value"}))

	last := machine.commands[len(machine.commands)-1]
	assert.Equal(t, "tool", last.Name)
	assert.Equal(t, []string{"--flag", "value"}, last.Args)
	assert.ElementsMatch(t,
		[]string{"CONSUL_HTTP_TOKEN=tok", "SETUP_CONSUL_TOKEN=tok", "LOCAL_IP=10.0.0.5"}, last.Env)
}
