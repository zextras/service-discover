// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/consul/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func covContext(t *testing.T, m *fakeMachine, name string) *setupContext {
	t.Helper()

	return &setupContext{env: m.env, def: covFindDef(t, name), token: "tok", log: noLog}
}

func TestCovSteps_prepareDocsEditorFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		corrupt func(t *testing.T, m *fakeMachine)
		want    string
		wantErr error
	}{
		{"template", func(t *testing.T, m *fakeMachine) {
			t.Helper()
			require.NoError(t, os.RemoveAll(filepath.Join(m.env.ServicesDir, "templates")))
		}, "unable to read the template", fs.ErrNotExist},
		{"no access to Consul", func(t *testing.T, m *fakeMachine) {
			t.Helper()
			m.env.Consul = func(string) (Consul, error) { return nil, errCov }
		}, errCov.Error(), errCov},
		{"catalog", func(t *testing.T, m *fakeMachine) {
			t.Helper()
			m.useConsul(&covConsul{instancesErr: errCov})
		}, "unable to read the instances of carbonio-docs-editor", errCov},
		{"definition cannot be written", func(t *testing.T, m *fakeMachine) {
			t.Helper()
			m.failChown(isTemporary)
		}, errCov.Error(), errCov},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := docsEditorMachine(t)
			tt.corrupt(t, machine)

			err := prepareDocsEditor(covContext(t, machine, "carbonio-docs-editor"))
			require.ErrorContains(t, err, tt.want)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestCovSteps_finishDocsEditorFailures(t *testing.T) {
	t.Parallel()

	withID := []Instance{{Address: "10.0.0.9", Meta: map[string]string{docsEditorMetaKey: "abc"}}}

	tests := []struct {
		name   string
		consul Consul
		noConn bool
		want   string
	}{
		{name: "no access to Consul", noConn: true, want: errCov.Error()},
		{name: "catalog", consul: &covConsul{instancesErr: errCov}, want: "unable to read the instances"},
		{name: "resolver", consul: &covConsul{
			instances: withID, entryFail: map[string]error{api.ServiceResolver: errCov},
		}, want: "unable to write the resolver"},
		{name: "router", consul: &covConsul{
			instances: withID, entryFail: map[string]error{api.ServiceRouter: errCov},
		}, want: "unable to write the router"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := docsEditorMachine(t)

			if tt.noConn {
				machine.env.Consul = func(string) (Consul, error) { return nil, errCov }
			} else {
				machine.useConsul(tt.consul)
			}

			err := finishDocsEditor(covContext(t, machine, "carbonio-docs-editor"))
			require.ErrorIs(t, err, errCov)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCovSteps_writeRoutingWithoutInstancesWritesNothing(t *testing.T) {
	t.Parallel()

	consul := &covConsul{}

	require.NoError(t, writeRouting(consul, "svc", nil, func(string) api.ServiceRouteHTTPMatch {
		t.Fatal("no match is built without an instance")

		return api.ServiceRouteHTTPMatch{}
	}, "key"))
	assert.Empty(t, consul.entries)
}

func TestCovSteps_writeRoutingSendsEachInstanceToItsSubset(t *testing.T) {
	t.Parallel()

	consul := &covConsul{}

	require.NoError(t, writeRouting(consul, "svc", []string{"a", "b"}, func(id string) api.ServiceRouteHTTPMatch {
		return api.ServiceRouteHTTPMatch{Header: []api.ServiceRouteHTTPMatchHeader{{Name: "id", Exact: id}}}
	}, "meta"))

	require.Len(t, consul.entries, 2)

	resolver, ok := consul.entries[0].(*api.ServiceResolverConfigEntry)
	require.True(t, ok)
	assert.Equal(t, `Service.Meta.meta == "a"`, resolver.Subsets["a"].Filter)
	assert.Equal(t, `Service.Meta.meta == "b"`, resolver.Subsets["b"].Filter)

	router, ok := consul.entries[1].(*api.ServiceRouterConfigEntry)
	require.True(t, ok)
	require.Len(t, router.Routes, 2)
	assert.Equal(t, "b", router.Routes[1].Destination.ServiceSubset)
	assert.Equal(t, "b", router.Routes[1].Match.HTTP.Header[0].Exact)
}

func TestCovSteps_prepareTurnServerFailures(t *testing.T) {
	t.Parallel()

	t.Run("no user", func(t *testing.T) {
		t.Parallel()

		machine := turnServerMachine(t, "turn.example.com", "turn.example.com")
		ctx := covContext(t, machine, "carbonio-turn-server")
		delete(machine.users, ctx.def.User)

		require.ErrorIs(t, prepareTurnServer(ctx), fs.ErrNotExist)
	})

	t.Run("no certificate key yet", func(t *testing.T) {
		t.Parallel()

		machine := turnServerMachine(t, "turn.example.com", "turn.example.com")
		ctx := covContext(t, machine, "carbonio-turn-server")
		require.NoError(t, os.WriteFile(joinCarbonio(machine.env, "turn-server", "turnserver.conf"), nil, 0o640))
		machine.env.Chown = func(string, int, int) error { return fs.ErrNotExist }

		logger := &covLogger{}
		ctx.log = logger.log

		require.NoError(t, prepareTurnServer(ctx))
		require.Len(t, logger.lines, 1)
		assert.Contains(t, logger.lines[0], "its owner is left alone")
	})

	t.Run("key that cannot be given", func(t *testing.T) {
		t.Parallel()

		machine := turnServerMachine(t, "turn.example.com", "turn.example.com")
		ctx := covContext(t, machine, "carbonio-turn-server")
		require.NoError(t, os.WriteFile(joinCarbonio(machine.env, "turn-server", "turnserver.conf"), nil, 0o640))
		machine.env.Chown = func(string, int, int) error { return errCov }

		err := prepareTurnServer(ctx)
		require.ErrorIs(t, err, errCov)
		require.ErrorContains(t, err, "unable to give the TLS key")
	})

	t.Run("certificate not covering the host", func(t *testing.T) {
		t.Parallel()

		machine := turnServerMachine(t, "turn.example.com", "other.example.com")

		err := prepareTurnServer(covContext(t, machine, "carbonio-turn-server"))
		require.ErrorIs(t, err, errCertificate)
	})

	t.Run("template missing", func(t *testing.T) {
		t.Parallel()

		machine := turnServerMachine(t, "turn.example.com", "turn.example.com")
		require.NoError(t, os.Remove(joinCarbonio(machine.env, "turn-server", "turnserver.conf.template")))

		err := prepareTurnServer(covContext(t, machine, "carbonio-turn-server"))
		require.ErrorContains(t, err, "unable to read the template of the configuration")
	})
}

func TestCovSteps_debconfValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		answer string
		err    error
		want   string
	}{
		{"answer", "0 203.0.113.7\n", nil, "203.0.113.7"},
		{"padded answer", "0   203.0.113.7  \n", nil, "203.0.113.7"},
		{"no such question", "10 videoserver/public-ip doesn't exist\n", nil, ""},
		{"no debconf", "", errCommand, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := newMachine(t)
			machine.exec = func(cmd *Command) ([]byte, error) {
				assert.Equal(t, "GET videoserver/public-ip\n", cmd.Stdin)

				return []byte(tt.answer), tt.err
			}

			assert.Equal(t, tt.want, debconfValue(machine.env, "videoserver/public-ip"))
		})
	}
}

func TestCovSteps_checkCertificate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	notPEM := filepath.Join(dir, "not-pem")
	garbage := filepath.Join(dir, "garbage")
	noNames := filepath.Join(dir, "no-names")
	named := filepath.Join(dir, "named")

	require.NoError(t, os.WriteFile(notPEM, []byte("plain text"), 0o644))
	require.NoError(t, os.WriteFile(garbage, []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), 0o644))
	selfSignedCertificate(t, noNames)
	selfSignedCertificate(t, named, "a.example.com")

	tests := []struct {
		name    string
		path    string
		host    string
		wantErr error
		message string
	}{
		{name: "missing", path: filepath.Join(dir, "missing"), host: "h", wantErr: errUnreadable},
		{name: "unreadable", path: dir, host: "h", message: "is a directory"},
		{name: "not PEM", path: notPEM, host: "h", wantErr: errUnreadable},
		{name: "not a certificate", path: garbage, host: "h", message: garbage},
		{name: "name covered", path: named, host: "a.example.com"},
		{name: "name not covered", path: named, host: "b.example.com", wantErr: errCertificate},
		{name: "common name only, matching", path: noNames, host: "cert"},
		{name: "common name only, not matching", path: noNames, host: "other", wantErr: errCertificate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := checkCertificate(tt.path, tt.host)

			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.message != "":
				require.ErrorContains(t, err, tt.message)
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestCovSteps_matchesName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern, host string
		want          bool
	}{
		{"mail.example.com", "mail.example.com", true},
		{"Mail.Example.COM", "mail.example.com", true},
		{"*.example.com", "mail.example.com", true},
		{"*.EXAMPLE.com", "mail.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "mail.other.org", false},
		{"mail.example.com", "smtp.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+" "+tt.host, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, matchesName(tt.pattern, tt.host))
		})
	}
}

func TestCovSteps_prepareAlertmanagerFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		corrupt func(t *testing.T, m *fakeMachine, ctx *setupContext)
		want    string
		wantErr error
	}{
		{"no user", func(_ *testing.T, m *fakeMachine, ctx *setupContext) {
			delete(m.users, ctx.def.User)
		}, "", fs.ErrNotExist},
		{"configuration of the package missing", func(t *testing.T, m *fakeMachine, ctx *setupContext) {
			t.Helper()
			require.NoError(t, os.Remove(joinCarbonio(m.env, ctx.def.Dir, "config-templates", "alertmanager.yml")))
		}, "unable to read the configuration of the package", fs.ErrNotExist},
		{"no access to Consul", func(_ *testing.T, m *fakeMachine, _ *setupContext) {
			m.env.Consul = func(string) (Consul, error) { return nil, errCov }
		}, "", errCov},
		{"store", func(_ *testing.T, m *fakeMachine, _ *setupContext) {
			m.useConsul(&covConsul{putErr: errCov})
		}, "unable to store the configuration", errCov},
		{"verification", func(_ *testing.T, m *fakeMachine, _ *setupContext) {
			m.useConsul(&covConsul{getErr: errCov})
		}, "unable to verify the stored configuration", errCov},
		{"stored value differs", func(_ *testing.T, m *fakeMachine, _ *setupContext) {
			m.useConsul(&covConsul{stale: "something else"})
		}, "the stored configuration does not match", errNoValue},
		{"candidate cannot be written", func(_ *testing.T, m *fakeMachine, _ *setupContext) {
			m.failChown(isTemporary)
		}, "", errCov},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			machine := alertmanagerMachine(t, nil)
			ctx := covContext(t, machine, "carbonio-prometheus-alertmanager")
			tt.corrupt(t, machine, ctx)

			err := prepareAlertmanager(ctx)
			require.ErrorIs(t, err, tt.wantErr)

			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
			}

			assert.NoFileExists(t, joinCarbonio(machine.env, ctx.def.Dir, "alertmanager.yml"))
		})
	}
}

func TestCovSteps_substituteFailures(t *testing.T) {
	t.Parallel()

	t.Run("file that cannot be read", func(t *testing.T) {
		t.Parallel()

		machine, path := prometheusMachine(t)
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.MkdirAll(path, 0o755))

		changed, err := substitute(machine.env, covFindDef(t, "carbonio-prometheus"), "tok", "", noLog)
		require.Error(t, err)
		assert.False(t, changed)
	})

	t.Run("file that cannot be replaced", func(t *testing.T) {
		t.Parallel()

		machine, path := prometheusMachine(t)
		machine.failChown(isTemporary)

		changed, err := substitute(machine.env, covFindDef(t, "carbonio-prometheus"), "tok", "", noLog)
		require.ErrorIs(t, err, errCov)
		assert.False(t, changed)

		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, prometheusConfig, string(content), "the file keeps its placeholders")
	})

	t.Run("nothing to replace", func(t *testing.T) {
		t.Parallel()

		machine, path := prometheusMachine(t)
		require.NoError(t, os.WriteFile(path, []byte("no placeholder here\n"), 0o640))

		changed, err := substitute(machine.env, covFindDef(t, "carbonio-prometheus"), "tok", "previous-token", noLog)
		require.NoError(t, err)
		assert.False(t, changed)
	})

	t.Run("a previous token is replaced", func(t *testing.T) {
		t.Parallel()

		machine, path := prometheusMachine(t)
		require.NoError(t, os.WriteFile(path, []byte("token: old\n"), 0o640))

		changed, err := substitute(machine.env, covFindDef(t, "carbonio-prometheus"), "new", "old", noLog)
		require.NoError(t, err)
		assert.True(t, changed)

		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, "token: new\n", string(content))
	})
}

func TestCovSteps_execCommand(t *testing.T) {
	t.Parallel()

	t.Run("output is returned", func(t *testing.T) {
		t.Parallel()

		out, err := execCommand(&Command{Name: "sh", Args: []string{"-c", "echo $COV_VAR; echo err >&2"}, Env: []string{"COV_VAR=value"}})
		require.NoError(t, err)
		assert.Equal(t, "value\nerr\n", string(out))
	})

	t.Run("standard input", func(t *testing.T) {
		t.Parallel()

		out, err := execCommand(&Command{Name: "cat", Stdin: "from stdin"})
		require.NoError(t, err)
		assert.Equal(t, "from stdin", string(out))
	})

	t.Run("output is streamed to the writer", func(t *testing.T) {
		t.Parallel()

		var buffer bytes.Buffer

		out, err := execCommand(&Command{Name: "sh", Args: []string{"-c", "echo out; echo err >&2"}, Out: &buffer})
		require.NoError(t, err)
		assert.Nil(t, out)
		assert.Equal(t, "out\nerr\n", buffer.String())
	})

	t.Run("failure carries the output", func(t *testing.T) {
		t.Parallel()

		out, err := execCommand(&Command{Name: "sh", Args: []string{"-c", "echo boom; exit 3"}})
		require.Error(t, err)
		require.ErrorContains(t, err, "boom")
		assert.Equal(t, "boom\n", string(out))

		var exit interface{ ExitCode() int }

		require.ErrorAs(t, err, &exit)
		assert.Equal(t, 3, exit.ExitCode())
	})

	t.Run("failure with a writer", func(t *testing.T) {
		t.Parallel()

		var buffer bytes.Buffer

		_, err := execCommand(&Command{Name: "sh", Args: []string{"-c", "echo boom; exit 1"}, Out: &buffer})
		require.Error(t, err)
		assert.Equal(t, "boom\n", buffer.String())
	})

	t.Run("unknown program", func(t *testing.T) {
		t.Parallel()

		_, err := execCommand(&Command{Name: "/nonexistent/cov-program"})
		require.ErrorIs(t, err, fs.ErrNotExist)
	})
}
