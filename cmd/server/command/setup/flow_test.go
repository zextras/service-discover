// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	osexec "os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/cmd/server/config"
	"github.com/zextras/service-discover/pkg/carbonio"
	carbonioMocks "github.com/zextras/service-discover/pkg/carbonio/mocks"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/pkg/exec"
	"github.com/zextras/service-discover/pkg/spire"
	"github.com/zextras/service-discover/pkg/systemd"
	systemdMocks "github.com/zextras/service-discover/pkg/systemd/mocks"
	"github.com/zextras/service-discover/test"
)

const (
	flowHostname = "mail.example.com"
	flowBindIP   = "10.0.0.5"
)

// stubCmd is an exec.Cmd whose output is computed by a function when the command is executed.
type stubCmd struct {
	run func() ([]byte, error)
}

func (c stubCmd) String() string                     { return "stub" }
func (c stubCmd) Run() error                         { _, err := c.run(); return err }
func (c stubCmd) Start() error                       { return nil }
func (c stubCmd) Wait() error                        { return nil }
func (c stubCmd) Output() ([]byte, error)            { return c.run() }
func (c stubCmd) CombinedOutput() ([]byte, error)    { return c.run() }
func (c stubCmd) StdinPipe() (io.WriteCloser, error) { return nil, errors.New("not implemented") }
func (c stubCmd) StdoutPipe() (io.ReadCloser, error) { return nil, errors.New("not implemented") }
func (c stubCmd) StderrPipe() (io.ReadCloser, error) { return nil, errors.New("not implemented") }

// flowDeps fakes the system the setup commands run on. Unimplemented dependencies panic when used.
type flowDeps struct {
	businessDependencies

	ldap     carbonio.LdapHandler
	fail     map[string]error
	unit     *systemdMocks.UnitManager
	handler  func(args []string) ([]byte, error)
	commands []string
}

func (d *flowDeps) LdapHandler(carbonio.LocalConfig) carbonio.LdapHandler { return d.ldap }

func (d *flowDeps) SystemdUnitHandler() (systemd.UnitManager, error) { return d.unit, nil }

func (d *flowDeps) CreateCommand(name string, args ...string) exec.Cmd {
	all := append([]string{name}, args...)
	d.commands = append(d.commands, strings.Join(all, " "))

	return stubCmd{run: func() ([]byte, error) { return d.handler(all) }}
}

func (d *flowDeps) ConsulLeader() (string, error) { return "10.0.0.5:8300", nil }

func (d *flowDeps) LookupUser(string) (*user.User, error) {
	return &user.User{Uid: "10", Gid: "20"}, d.fail["LookupUser"]
}

func (d *flowDeps) LookupGroup(string) (*user.Group, error) { return &user.Group{Gid: "20"}, nil }

func (d *flowDeps) Chown(string, int, int) error { return nil }

func (d *flowDeps) Chmod(string, os.FileMode) error { return nil }

func (d *flowDeps) NetInterfaces() ([]net.Interface, error) {
	return []net.Interface{{Name: "lo"}, {Name: "eth0"}}, nil
}

func (d *flowDeps) AddrResolver(net.Interface) ([]net.Addr, error) {
	return []net.Addr{&addrStub{ip: flowBindIP + "/24"}}, nil
}

func (d *flowDeps) LookupIP(string) ([]net.IP, error) { return []net.IP{net.ParseIP(flowBindIP)}, nil }

func (d *flowDeps) ranCommand(prefix string) bool {
	for _, c := range d.commands {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}

	return false
}

type flowEnv struct {
	t          *testing.T
	setup      *Setup
	deps       *flowDeps
	ldap       *carbonioMocks.LdapHandler
	uploaded   []byte
	downloaded []byte
	failOn     string
}

const flowLocalConfig = `<?xml version="1.0" encoding="UTF-8"?>
<localconfig>
  <key name="ldap_master_url"><value>ldap://mail.example.com:389</value></key>
  <key name="ldap_url"><value>ldap://mail.example.com:389</value></key>
  <key name="zimbra_ldap_userdn"><value>uid=zimbra,cn=admins,cn=zimbra</value></key>
  <key name="zimbra_ldap_password"><value>password</value></key>
  <key name="zimbra_server_hostname"><value>mail.example.com</value></key>
</localconfig>`

func newFlowEnv(t *testing.T) *flowEnv {
	t.Helper()

	previousTesting := testingMode
	testingMode = true
	previousJWKS := waitJWKS
	previousPoll, previousTimeout := spirePollInterval, spireStartTimeout
	spirePollInterval, spireStartTimeout = time.Millisecond, 20*time.Millisecond
	waitJWKS = func(string, []byte, time.Duration, time.Duration) error { return nil }

	t.Cleanup(func() {
		testingMode = previousTesting
		waitJWKS = previousJWKS
		spirePollInterval, spireStartTimeout = previousPoll, previousTimeout
	})

	dir := t.TempDir()
	consulHome := filepath.Join(dir, "consul")
	require.NoError(t, os.MkdirAll(consulHome, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "etc"), 0o755))

	localConfig := filepath.Join(dir, "localconfig.xml")
	require.NoError(t, os.WriteFile(localConfig, []byte(flowLocalConfig), 0o600))

	env := &flowEnv{
		t: t,
		setup: &Setup{
			ConsulConfigDir:   filepath.Join(dir, "etc"),
			ConsulHome:        consulHome,
			LocalConfigPath:   localConfig,
			ConsulData:        filepath.Join(consulHome, "data"),
			ConsulFileConfig:  filepath.Join(dir, "etc", "config.json"),
			ClusterCredential: filepath.Join(dir, "cluster-credentials.tar.gpg"),
			MutableConfigFile: filepath.Join(dir, "etc", "mutable.json"),
			SpireConfigDir:    filepath.Join(dir, "spire"),
			Password:          "pw",
			BindAddress:       flowBindIP,
		},
		ldap: new(carbonioMocks.LdapHandler),
	}

	env.ldap.On("CheckServerAvailability", mock.Anything).Return(nil)
	env.ldap.On("AddService", flowHostname, mock.Anything).Return(nil)
	env.ldap.On("UploadBinary", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		content, err := io.ReadAll(args.Get(0).(io.Reader))
		require.NoError(t, err)

		env.uploaded = content
	}).Return(nil)
	env.ldap.On("DownloadBinary", mock.Anything, mock.Anything).Return(
		func(string, string) []byte { return env.downloaded }, nil)

	unit := new(systemdMocks.UnitManager)
	unit.On("StartUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
		args.Get(2).(chan<- string) <- "done"
	}).Return(0, nil)
	unit.On("EnableUnitFiles", mock.Anything, false, false).Return(false, nil, nil)
	unit.On("Close").Return()

	env.deps = &flowDeps{ldap: env.ldap, unit: unit, handler: env.handleCommand}

	return env
}

// handleCommand answers the commands of consul and spire-server the setup runs.
func (e *flowEnv) handleCommand(args []string) ([]byte, error) {
	line := strings.Join(args, " ")

	if e.failOn != "" && strings.Contains(line, e.failOn) {
		return nil, &osexec.ExitError{Stderr: []byte("command failed: " + line)}
	}

	switch {
	case strings.HasPrefix(line, command.ConsulBin+" tls ca create"):
		e.writeCA()
	case strings.HasPrefix(line, command.ConsulBin+" acl bootstrap"):
		return []byte(`{"SecretID": "root-token"}`), nil
	case strings.HasPrefix(line, command.ConsulBin+" acl token create"):
		return []byte(`{"SecretID": "server-token"}`), nil
	case strings.HasPrefix(line, spire.ServerBin+" bundle show"):
		return []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), nil
	case strings.HasPrefix(line, spire.ServerBin+" token generate"):
		return []byte(`{"value":"join-me"}`), nil
	}

	return []byte("ok"), nil
}

func (e *flowEnv) writeCA() {
	writeTestCA(e.t, e.setup.ConsulHome)
}

func TestSetup_firstSetup(t *testing.T) {
	t.Run("sets up the first server, publishing the cluster credentials", func(t *testing.T) {
		env := newFlowEnv(t)

		out, err := env.setup.firstSetup(env.deps)
		require.NoError(t, err)

		rendered, err := out.JSONRender()
		require.NoError(t, err)
		assert.JSONEq(t, `{"cluster_credentials":"`+env.setup.ClusterCredential+`"}`, rendered)

		cfg, err := os.ReadFile(env.setup.ConsulFileConfig)
		require.NoError(t, err)
		assert.Contains(t, string(cfg), `"auto_config"`)
		assert.Contains(t, string(cfg), "https://mail.example.com:8082")

		mutable, err := os.ReadFile(env.setup.MutableConfigFile)
		require.NoError(t, err)
		assert.Contains(t, string(mutable), flowBindIP)

		password, err := os.ReadFile(filepath.Join(env.setup.ConsulHome, "password"))
		require.NoError(t, err)
		assert.Equal(t, "pw", string(password))

		for _, prefix := range []string{
			"/usr/bin/consul tls ca create",
			"/usr/bin/consul tls cert create",
			"/usr/bin/consul acl bootstrap",
			"/usr/bin/consul acl set-agent-token default server-token",
			"/usr/bin/consul acl auth-method read",
			spire.ServerBin + " token generate",
		} {
			assert.True(t, env.deps.ranCommand(prefix), prefix)
		}

		env.ldap.AssertCalled(t, "AddService", flowHostname, carbonio.ServiceDiscoverServiceName)

		// The credentials uploaded to LDAP carry everything the other servers need.
		reader, err := encrypter.NewReader(bytes.NewReader(env.uploaded), []byte("pw"))
		require.NoError(t, err)

		files, err := encrypter.ReadFiles(reader,
			spire.ClusterJWKSURLFile, command.GossipKey, command.ConsulACLBootstrap,
		)
		require.NoError(t, err)
		assert.Equal(t, "https://mail.example.com:8082/keys", string(files[spire.ClusterJWKSURLFile]))
		assert.JSONEq(t, `{"SecretID": "root-token"}`, string(files[command.ConsulACLBootstrap]))
		assert.NotEmpty(t, files[command.GossipKey])
	})

	t.Run("fails on an address that does not belong to this machine", func(t *testing.T) {
		env := newFlowEnv(t)
		env.setup.BindAddress = "192.168.1.1"

		_, err := env.setup.firstSetup(env.deps)

		assert.EqualError(t, err, "invalid binding address selected")
	})
}

func TestSetup_performSetup_failures(t *testing.T) {
	tests := []struct {
		name    string
		failOn  string
		prepare func(env *flowEnv)
		wantErr string
	}{
		{name: "missing localconfig", prepare: func(env *flowEnv) {
			env.setup.LocalConfigPath = filepath.Join(env.setup.ConsulHome, "missing.xml")
		}, wantErr: "unable to parse local config"},
		{name: "CA creation fails", failOn: "tls ca create", wantErr: "unable to create a valid CA with Consul"},
		{name: "SPIRE server fails", failOn: "healthcheck", wantErr: "unable to set up the SPIRE server"},
		{name: "server certificate fails", failOn: "tls cert create", wantErr: "unable to create a valid certificate"},
		{name: "ACL bootstrap fails", failOn: "acl bootstrap", wantErr: "unable to create ACL bootstrap token"},
		{name: "auth method fails", failOn: "acl auth-method", wantErr: "auth method"},
		{name: "SPIRE agent fails", failOn: "token generate", wantErr: "unable to set up the SPIRE agent"},
		{name: "LDAP upload fails", prepare: func(env *flowEnv) {
			env.ldap = new(carbonioMocks.LdapHandler)
			env.ldap.On("CheckServerAvailability", mock.Anything).Return(nil).Once()
			env.ldap.On("CheckServerAvailability", mock.Anything).Return(errors.New("ldap down"))
			env.ldap.On("AddService", mock.Anything, mock.Anything).Return(nil)
			env.deps.ldap = env.ldap
		}, wantErr: "ldap down"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newFlowEnv(t)
			env.failOn = tt.failOn

			if tt.prepare != nil {
				tt.prepare(env)
			}

			err := env.setup.performSetup(env.deps, &setupConfiguration{Password: "pw", BindAddress: flowBindIP})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// credentialsTarball builds the cluster credentials as the first server publishes them.
func credentialsTarball(t *testing.T, password string, files map[string]string) []byte {
	t.Helper()

	var out bytes.Buffer

	writer, err := encrypter.NewWriter(&out, []byte(password))
	require.NoError(t, err)

	for name, content := range files {
		buf, info := test.CreateDumbFile([]byte(content), name)
		require.NoError(t, writer.AddFile(buf, info, name, "/"))
	}

	require.NoError(t, writer.Flush())
	writer.Close()

	return out.Bytes()
}

func clusterFiles(t *testing.T, jwksURL string) map[string]string {
	t.Helper()

	rel := func(name string) string {
		path, err := filepath.Rel("/", filepath.Join(config.ConsulHome, name))
		require.NoError(t, err)

		return path
	}

	dir := t.TempDir()
	writeTestCA(t, dir)

	ca, err := os.ReadFile(filepath.Join(dir, command.ConsulCA))
	require.NoError(t, err)

	caKey, err := os.ReadFile(filepath.Join(dir, command.ConsulCAKey))
	require.NoError(t, err)

	files := map[string]string{
		rel(command.ConsulCA):      string(ca),
		rel(command.ConsulCAKey):   string(caKey),
		command.ConsulACLBootstrap: `{"SecretID": "root-token"}`,
		command.GossipKey:          "gossip",
	}

	if jwksURL != "" {
		files[spire.ClusterJWKSURLFile] = jwksURL
	}

	return files
}

func TestSetup_importSetup_flow(t *testing.T) {
	t.Run("joins the cluster with the credentials downloaded from LDAP", func(t *testing.T) {
		env := newFlowEnv(t)
		env.downloaded = credentialsTarball(t, "pw", clusterFiles(t, "https://mail.example.com:8082"))
		require.NoError(t, os.WriteFile(env.setup.ClusterCredential, nil, 0o600))

		out, err := env.setup.importSetup(env.deps)
		require.NoError(t, err)
		assert.NotNil(t, out)

		cfg, err := os.ReadFile(env.setup.ConsulFileConfig)
		require.NoError(t, err)
		assert.Contains(t, string(cfg), `"auto_config"`)
		assert.Contains(t, string(cfg), `"encrypt": "gossip"`)

		_, err = os.Stat(filepath.Join(env.setup.ConsulHome, command.ConsulCA))
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(env.setup.ConsulHome, command.ConsulCAKey))
		assert.True(t, os.IsNotExist(err), "the CA key must not stay on the other servers")

		assert.True(t, env.deps.ranCommand("/usr/bin/consul acl set-agent-token default server-token"))
		assert.False(t, env.deps.ranCommand("/usr/bin/consul acl bootstrap"))
		env.ldap.AssertCalled(t, "AddService", flowHostname, carbonio.ServiceDiscoverServiceName)
	})

	t.Run("a cluster without SPIRE keeps the previous configuration", func(t *testing.T) {
		env := newFlowEnv(t)
		env.downloaded = credentialsTarball(t, "pw", clusterFiles(t, ""))

		_, err := env.setup.importSetup(env.deps)
		require.NoError(t, err)

		cfg, err := os.ReadFile(env.setup.ConsulFileConfig)
		require.NoError(t, err)
		assert.NotContains(t, string(cfg), "auto_config")
	})

	failures := []struct {
		name    string
		prepare func(env *flowEnv)
		failOn  string
		wantErr string
	}{
		{name: "wrong password", prepare: func(env *flowEnv) { env.setup.Password = "wrong" }, wantErr: "unable to open"},
		{name: "unreadable credentials", prepare: func(env *flowEnv) {
			env.downloaded = []byte("not a credentials archive")
		}, wantErr: "unable to open"},
		{name: "incomplete credentials", prepare: func(env *flowEnv) {
			env.downloaded = credentialsTarball(t, "pw", map[string]string{command.GossipKey: "gossip"})
		}, wantErr: "not all files were found"},
		{name: "invalid bootstrap token", prepare: func(env *flowEnv) {
			files := clusterFiles(t, "")
			files[command.ConsulACLBootstrap] = "not json"
			env.downloaded = credentialsTarball(t, "pw", files)
		}, wantErr: "unable to decode ACL Bootstrap token"},
		{name: "server certificate fails", failOn: "tls cert create", wantErr: "unable to create a valid certificate"},
		{name: "invalid binding address", prepare: func(env *flowEnv) { env.setup.BindAddress = "1.2.3.4" },
			wantErr: "invalid binding address selected"},
	}

	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			env := newFlowEnv(t)
			env.downloaded = credentialsTarball(t, "pw", clusterFiles(t, "https://mail.example.com:8082"))
			env.failOn = tt.failOn

			if tt.prepare != nil {
				tt.prepare(env)
			}

			_, err := env.setup.importSetup(env.deps)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSetup_createClusterCredentials(t *testing.T) {
	env := newFlowEnv(t)
	env.writeCA()

	err := env.setup.createClusterCredentials(sharedSecrets{
		aclBootstrapJSON: []byte(`{"SecretID":"s"}`), gossipKey: "k", jwksURL: "https://h:8082",
	}, "pw")
	require.NoError(t, err)

	credentials, err := os.Open(env.setup.ClusterCredential)
	require.NoError(t, err)

	defer credentials.Close()

	reader, err := encrypter.NewReader(credentials, []byte("pw"))
	require.NoError(t, err)

	files, err := encrypter.ReadFiles(reader,
		spire.ClusterJWKSURLFile, command.GossipKey, command.ConsulACLBootstrap,
		strings.TrimPrefix(env.setup.ConsulHome+"/"+command.ConsulCA, "/"),
		strings.TrimPrefix(env.setup.ConsulHome+"/"+command.ConsulCAKey, "/"),
	)
	require.NoError(t, err)
	assert.Equal(t, "k", string(files[command.GossipKey]))
	assert.Equal(t, "https://h:8082", string(files[spire.ClusterJWKSURLFile]))

	t.Run("fails when the CA is missing", func(t *testing.T) {
		env := newFlowEnv(t)

		err := env.setup.createClusterCredentials(sharedSecrets{}, "pw")

		assert.ErrorContains(t, err, "unable to open")
	})
}

func TestSetup_bootstrapACL_invalidResponse(t *testing.T) {
	env := newFlowEnv(t)
	env.deps.handler = func([]string) ([]byte, error) { return []byte("not json"), nil }

	_, _, err := env.setup.bootstrapACL(env.deps, flowHostname)

	assert.ErrorContains(t, err, "unable to decode ACL bootstrap response from Consul")
}

func TestSetup_setupSpireIntegration(t *testing.T) {
	t.Run("fails without the Consul CA", func(t *testing.T) {
		env := newFlowEnv(t)

		err := env.setup.setupSpireIntegration(env.deps, flowHostname, flowBindIP, "https://h:8082", "root")

		assert.ErrorContains(t, err, "unable to read the Consul CA")
	})

	t.Run("does not touch the SPIRE units inside a container", func(t *testing.T) {
		env := newFlowEnv(t)
		testingMode = false

		env.writeCA()

		err := env.setup.setupSpireIntegration(env.deps, flowHostname, flowBindIP, "https://h:8082", "root")

		require.NoError(t, err)
		assert.Equal(t, !command.CheckDockerContainer(), env.deps.ranCommand(spire.ServerBin+" token generate"))
	})
}

func TestSetup_startServiceDiscoverDaemon(t *testing.T) {
	env := newFlowEnv(t)

	isContainer, err := startServiceDiscoverDaemon(env.deps)

	require.NoError(t, err)
	assert.Equal(t, command.CheckDockerContainer(), isContainer)
	env.deps.unit.AssertCalled(t, "StartUnit", serviceDiscoverUnit, "replace", mock.Anything)
}

func TestSetup_setupSpireServer_failures(t *testing.T) {
	tests := []struct {
		name    string
		bind    string
		prepare func(env *flowEnv)
		failOn  string
		wantErr string
	}{
		{name: "invalid binding address", bind: "not-an-ip", wantErr: "invalid binding address 'not-an-ip'"},
		{name: "missing CA", prepare: func(env *flowEnv) {
			require.NoError(t, os.Remove(filepath.Join(env.setup.ConsulHome, command.ConsulCA)))
		}, wantErr: "unable to read the Consul CA"},
		{name: "missing CA key", prepare: func(env *flowEnv) {
			require.NoError(t, os.Remove(filepath.Join(env.setup.ConsulHome, command.ConsulCAKey)))
		}, wantErr: "unable to read the Consul CA key"},
		{name: "invalid CA", prepare: func(env *flowEnv) {
			require.NoError(t, os.WriteFile(filepath.Join(env.setup.ConsulHome, command.ConsulCA), []byte("junk"), 0o600))
		}, wantErr: "CA certificate"},
		{name: "configuration cannot be written", prepare: func(env *flowEnv) {
			require.NoError(t, os.WriteFile(env.setup.SpireConfigDir, nil, 0o600))
		}, wantErr: "unable to create"},
		{name: "ownership cannot be set", prepare: func(env *flowEnv) {
			env.deps.fail = map[string]error{"LookupUser": errors.New("no such user")}
		}, wantErr: "no such user"},
		{name: "unit does not start", prepare: func(env *flowEnv) {
			env.deps.unit = failingUnit(t, "StartUnit")
		}, wantErr: "unable to start " + spire.ServerUnit},
		{name: "unit cannot be enabled", prepare: func(env *flowEnv) {
			env.deps.unit = failingUnit(t, "EnableUnitFiles")
		}, wantErr: "unable to enable " + spire.ServerUnit},
		{name: "server never gets healthy", failOn: "healthcheck", wantErr: "spire-server is not healthy"},
		{name: "OIDC discovery never gets ready", prepare: func(*flowEnv) {
			waitJWKS = func(string, []byte, time.Duration, time.Duration) error {
				return errors.New("discovery timeout")
			}
		}, wantErr: "discovery timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newFlowEnv(t)
			env.writeCA()

			env.failOn = tt.failOn
			if tt.prepare != nil {
				tt.prepare(env)
			}

			bind := flowBindIP
			if tt.bind != "" {
				bind = tt.bind
			}

			_, err := env.setup.setupSpireServer(env.deps, flowHostname, bind, false)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSetup_setupSpireAgent_failures(t *testing.T) {
	tests := []struct {
		name    string
		bind    string
		prepare func(env *flowEnv)
		failOn  string
		wantErr string
	}{
		{name: "invalid binding address", bind: "not-an-ip", wantErr: "invalid binding address"},
		{name: "server never gets healthy", failOn: "healthcheck", wantErr: "spire-server is not healthy"},
		{name: "no trust bundle", failOn: "bundle show", wantErr: "unable to read the SPIRE trust bundle"},
		{name: "no join token", failOn: "token generate", wantErr: "unable to generate a SPIRE join token"},
		{name: "bundle cannot be written", prepare: func(env *flowEnv) {
			require.NoError(t, os.WriteFile(env.setup.SpireConfigDir, nil, 0o600))
		}, wantErr: "unable to create"},
		{name: "unit does not start", prepare: func(env *flowEnv) {
			env.deps.unit = failingUnit(t, "StartUnit")
		}, wantErr: "unable to start " + spire.AgentUnit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newFlowEnv(t)
			env.failOn = tt.failOn

			if tt.prepare != nil {
				tt.prepare(env)
			}

			bind := flowBindIP
			if tt.bind != "" {
				bind = tt.bind
			}

			err := env.setup.setupSpireAgent(env.deps, flowHostname, bind)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// failingUnit returns a unit manager where the named operation fails.
func failingUnit(t *testing.T, failing string) *systemdMocks.UnitManager {
	t.Helper()

	unit := new(systemdMocks.UnitManager)

	if failing == "StartUnit" {
		unit.On("StartUnit", mock.Anything, "replace", mock.Anything).Return(0, errors.New("dbus down"))
	} else {
		unit.On("StartUnit", mock.Anything, "replace", mock.Anything).Run(func(args mock.Arguments) {
			args.Get(2).(chan<- string) <- "done"
		}).Return(0, nil)
		unit.On("EnableUnitFiles", mock.Anything, false, false).Return(false, nil, errors.New("dbus down"))
	}

	unit.On("Close").Return()

	return unit
}

func TestSetup_importSetup_unitFailures(t *testing.T) {
	t.Run("the service does not start", func(t *testing.T) {
		env := newFlowEnv(t)
		env.downloaded = credentialsTarball(t, "pw", clusterFiles(t, ""))
		env.deps.unit = failingUnit(t, "StartUnit")

		_, err := env.setup.importSetup(env.deps)

		assert.ErrorContains(t, err, "unable to start "+serviceDiscoverUnit)
	})

	t.Run("the service cannot be enabled", func(t *testing.T) {
		if command.CheckDockerContainer() {
			t.Skip("the unit is not enabled inside a container")
		}

		env := newFlowEnv(t)
		env.downloaded = credentialsTarball(t, "pw", clusterFiles(t, ""))
		env.deps.unit = failingUnit(t, "EnableUnitFiles")

		_, err := env.setup.importSetup(env.deps)

		assert.ErrorContains(t, err, "unable to enable "+serviceDiscoverUnit)
	})

	t.Run("the credentials cannot be downloaded", func(t *testing.T) {
		env := newFlowEnv(t)
		env.ldap = new(carbonioMocks.LdapHandler)
		env.ldap.On("CheckServerAvailability", true).Return(nil)
		env.ldap.On("CheckServerAvailability", false).Return(errors.New("ldap down"))
		env.deps.ldap = env.ldap

		_, err := env.setup.importSetup(env.deps)

		assert.ErrorContains(t, err, "unable to download credentials from LDAP")
	})
}
