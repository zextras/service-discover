package main

import (
	"errors"
	"io"
	"os/user"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/zextras/service-discover/pkg/carbonio"
)

const (
	discoverdBinPath = "/usr/bin/service-discoverd"
)

type mockDependencies struct {
	mock.Mock
}

func (m *mockDependencies) Exit(code int) {
	m.Called(code)
}

func (m *mockDependencies) Log(a ...any) {
	m.Called(a)
}

func (m *mockDependencies) Getuid() (uid int) {
	args := m.Called()

	return args.Int(0)
}

func (m *mockDependencies) Getenv(key string) (env string) {
	args := m.Called(key)

	return args.String(0)
}

func (m *mockDependencies) UserLookup(username string) (*user.User, error) {
	args := m.Called(username)
	usr := args.Get(0)

	if usr == nil {
		return nil, args.Error(1)
	}

	return usr.(*user.User), args.Error(1)
}

func (m *mockDependencies) Setuid(uid int) (err error) {
	args := m.Called(uid)

	return args.Error(0)
}

func (m *mockDependencies) Setgid(gid int) (err error) {
	args := m.Called(gid)

	return args.Error(0)
}

func (m *mockDependencies) Exec(argv0 string, argv []string, env []string) (err error) {
	args := m.Called(argv0, argv, env)

	return args.Error(0)
}

func (m *mockDependencies) LoadLocalConfig() (carbonio.LocalConfig, error) {
	args := m.Called()
	localConfig := args.Get(0)

	if localConfig == nil {
		return nil, args.Error(1)
	}

	return localConfig.(carbonio.LocalConfig), nil
}

func (m *mockDependencies) CreateNewHandler(localConfig carbonio.LocalConfig) carbonio.LdapHandler {
	args := m.Called(localConfig)

	return args.Get(0).(carbonio.LdapHandler)
}

func (m *mockDependencies) Value(key string) string {
	args := m.Called(key)

	return args.String(0)
}

func (m *mockDependencies) Values(url string) []string {
	panic("should not be used")
}

func (m *mockDependencies) Text(key string) string {
	panic("should not be used")
}

func (m *mockDependencies) AddService(server string, service string) error {
	panic("should not be used")
}
func (m *mockDependencies) RemoveService(server string, service string) error {
	panic("should not be used")
}
func (m *mockDependencies) QueryAllServersWithService(service string) ([]string, error) {
	args := m.Called(service)
	servers := args.Get(0)

	if servers == nil {
		return nil, args.Error(1)
	}

	return servers.([]string), args.Error(1)
}
func (m *mockDependencies) CheckServerAvailability(write bool) error {
	panic("should not be used")
}

func (m *mockDependencies) UploadBinary(reader io.Reader, dn string, attribute string) error {
	panic("should not be used")
}

func (m *mockDependencies) DownloadBinary(dn string, attribute string) ([]byte, error) {
	panic("should not be used")
}

func Test_runServiceDiscoverDaemon(t *testing.T) {
	t.Run("server run", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		setupMock(mockDependencies, true)
		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "server"},
		)
		mockDependencies.AssertNumberOfCalls(t, "Exec", 1)
		mockDependencies.AssertCalled(t, "Exit", ExitCodeExecError)
	})

	t.Run("agent run", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		setupMock(mockDependencies, false)
		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "agent"},
		)
		mockDependencies.AssertNumberOfCalls(t, "Exec", 1)
		mockDependencies.AssertCalled(t, "Exit", ExitCodeExecError)
	})

	t.Run("missing argument", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		setupMock(mockDependencies, true)
		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath},
		)
		mockDependencies.AssertCalled(t, "Log", []any{"one parameter: server or agent"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeWrongArgs)
	})

	t.Run("wrong argument", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		setupMock(mockDependencies, true)
		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "invalid"},
		)
		mockDependencies.AssertCalled(t, "Log", []any{"one parameter: server or agent"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeWrongArgs)
	})

	t.Run("non root user", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		mockDependencies.On("Getuid").Return(1000)
		setupMock(mockDependencies, true)

		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "server"},
		)
		mockDependencies.AssertCalled(t, "Log", []any{"run as root"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeUserStuff)
	})

	t.Run("missing local config", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		mockDependencies.On("LoadLocalConfig").Return(nil, errors.New("fake error"))
		setupMock(mockDependencies, true)

		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "server"},
		)
		mockDependencies.AssertCalled(t, "Log", []any{"unable to read ldap configuration: fake error"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeLocalCfg)
	})

	t.Run("cannot find user", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		mockDependencies.On("UserLookup", "service-discover").Return(
			nil, errors.New("fake error"),
		)
		setupMock(mockDependencies, true)

		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "server"},
		)
		mockDependencies.AssertCalled(t, "Log", []any{"cannot find user 'service-discover': fake error"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeUserStuff)
	})

	t.Run("cannot set uid", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		mockDependencies.On("Setuid", 100).Return(errors.New("fake error"))
		setupMock(mockDependencies, true)

		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "server"},
		)
		mockDependencies.AssertCalled(t, "Log", []any{"cannot change uid: fake error"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeUserStuff)
	})

	t.Run("cannot query ldap", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		mockDependencies.On("QueryAllServersWithService", "service-discover").Return(
			nil,
			errors.New("fake error"),
		)
		setupMock(mockDependencies, true)

		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "server"},
		)
		mockDependencies.AssertCalled(t, "Log", []any{"unable to query ldap: fake error"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeLdapError)
	})

	t.Run("server missing in ldap", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		mockDependencies.On("QueryAllServersWithService", "service-discover").Return(
			[]string{"remote-hostname-1", "remote-hostname-2"},
			nil,
		)
		setupMock(mockDependencies, true)

		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "server"},
		)
		mockDependencies.AssertCalled(t, "Log",
			[]any{"local service-discover server NOT present in ldap/zimbraServiceEnabled attribute local-hostname"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeLdapError)
	})

	t.Run("agent present in ldap", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		mockDependencies.On("QueryAllServersWithService", "service-discover").Return(
			[]string{"local-hostname", "remote-hostname-1", "remote-hostname-2"},
			nil,
		)
		setupMock(mockDependencies, false)

		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "agent"},
		)
		mockDependencies.AssertCalled(t, "Log",
			[]any{"local service-discover agent must NOT be present in ldap/zimbraServiceEnabled attribute local-hostname"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeLdapError)
	})

	t.Run("exec failure", func(t *testing.T) {
		mockDependencies := new(mockDependencies)
		mockDependencies.On(
			"Exec",
			"/usr/bin/consul",
			[]string{
				"/usr/bin/consul",
				"agent",
				"-bootstrap-expect", "2",
				"-config-dir", "/etc/zextras/service-discover/",
				"-server",
				"-retry-join=local-hostname", // hack
				"-retry-join=remote-hostname-1",
				"-retry-join=remote-hostname-2",
			},
			[]string{
				"SHELL=/bin/custom-shell",
				"NOTIFY_SOCKET=/run/dbus/systemd-socket",
			},
		).Return(errors.New("fake error"))
		setupMock(mockDependencies, true)

		runServiceDiscoverDaemon(
			mockDependencies,
			[]string{discoverdBinPath, "server"},
		)
		mockDependencies.AssertCalled(t, "Log", []any{"consul execute failed: fake error"})
		mockDependencies.AssertCalled(t, "Exit", ExitCodeExecError)
	})
}

func setupMock(mockDependencies *mockDependencies, isServer bool) {
	mockDependencies.On("Getuid").Return(0)
	mockDependencies.On("Getenv", "SHELL").Return("/bin/custom-shell")
	mockDependencies.On("Getenv", "NOTIFY_SOCKET").Return("/run/dbus/systemd-socket")
	mockDependencies.On("LoadLocalConfig").Return(mockDependencies, nil)
	mockDependencies.On("CreateNewHandler", mockDependencies).Return(mockDependencies, nil)
	mockDependencies.On("Value", "zimbra_server_hostname").Return("local-hostname")
	mockDependencies.On("UserLookup", "service-discover").Return(
		&user.User{
			Uid:      "100",
			Gid:      "120",
			Username: "service-discover",
			Name:     "service-discover",
			HomeDir:  "/var/lib/service-discover/",
		},
		nil,
	)
	mockDependencies.On("Setgid", 120).Return(nil)
	mockDependencies.On("Setuid", 100).Return(nil)

	if isServer {
		mockDependencies.On("QueryAllServersWithService", "service-discover").Return(
			[]string{"local-hostname", "remote-hostname-1", "remote-hostname-2"},
			nil,
		)
		mockDependencies.On(
			"Exec",
			"/usr/bin/consul",
			[]string{
				"/usr/bin/consul",
				"agent",
				"-bootstrap-expect", "2",
				"-config-dir", "/etc/zextras/service-discover/",
				"-server",
				"-retry-join=local-hostname", // hack
				"-retry-join=remote-hostname-1",
				"-retry-join=remote-hostname-2",
			},
			[]string{
				"SHELL=/bin/custom-shell",
				"NOTIFY_SOCKET=/run/dbus/systemd-socket",
			},
		).Return(nil)
	} else {
		mockDependencies.On("QueryAllServersWithService", "service-discover").Return(
			[]string{"remote-hostname-1", "remote-hostname-2"},
			nil,
		)

		mockDependencies.On(
			"Exec",
			"/usr/bin/consul",
			[]string{
				"/usr/bin/consul",
				"agent",
				"-config-dir", "/etc/zextras/service-discover/",
				// "-retry-join=local-hostname", // FIXME ask to @Baldo if this entry should be there or not
				"-retry-join=remote-hostname-1",
				"-retry-join=remote-hostname-2",
			},
			[]string{
				"SHELL=/bin/custom-shell",
				"NOTIFY_SOCKET=/run/dbus/systemd-socket",
			},
		).Return(nil)
	}

	mockDependencies.On("Log", mock.AnythingOfType("[]interface {}")).Return()
	mockDependencies.On("Exit", mock.AnythingOfType("int")).Return()
}
