package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/test"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"io/ioutil"
	"net"
	"os"
	"testing"
)

type addrStub struct {
	ip string
}

func (a *addrStub) Network() string {
	return "tcp"
}

func (a *addrStub) String() string {
	return a.ip
}

type mockedNetworkInterfaces struct {
	mock.Mock
}

func (m *mockedNetworkInterfaces) AddrResolver(n net.Interface) ([]net.Addr, error) {
	args := m.Called(n)
	return args.Get(0).([]net.Addr), args.Error(1)
}

func (m *mockedNetworkInterfaces) NetInterfaces() ([]net.Interface, error) {
	args := m.Called()
	return args.Get(0).([]net.Interface), args.Error(1)
}

func (m *mockedNetworkInterfaces) LookupIP(s string) ([]net.IP, error) {
	ret := m.Called(s)

	var r0 []net.IP
	if rf, ok := ret.Get(0).(func(string) []net.IP); ok {
		r0 = rf(s)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]net.IP)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(s)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

func TestCheckValidBindingAddress(t *testing.T) {
	t.Parallel()

	t.Run("Valid interface selected", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1"},
				&addrStub{ip: "10.0.0.1"},
			},
			nil,
		)
		err := CheckValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"127.0.0.1",
		)
		assert.NoError(t, err)
	})

	t.Run("Invalid interface selected", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1"},
			},
			nil,
		)
		err := CheckValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"192.168.1.2", //random one, it doesn't really matter
		)
		assert.EqualError(t, err, "invalid binding address selected")
	})

	t.Run("Valid address selected with subnet", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1/24"},
			}, nil,
			nil,
		)
		err := CheckValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"127.0.0.1",
		)
		assert.NoError(t, err)
	})

	t.Run("Valid subnet selected with subnet", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1/24"},
			},
			nil,
		)
		err := CheckValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"127.0.0.1/24",
		)
		assert.NoError(t, err)
	})

	t.Run("Invalid subnet selected with subnet", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		networkInterface := net.Interface{
			Name: "lo",
		}
		mockDependencies.On("AddrResolver", networkInterface).Return(
			[]net.Addr{
				&addrStub{ip: "127.0.0.1/24"},
			}, nil,
		)
		err := CheckValidBindingAddress(
			mockDependencies,
			[]net.Interface{
				networkInterface,
			},
			"10.0.0.1/8",
		)
		assert.EqualError(t, err, "invalid binding address selected")
	})

	t.Run("Address resolution failure", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		mockDependencies.On("LookupIP", "address").Return([]net.IP{}, errors.New("random-failure"))
		err := CheckHostnameAddress(
			mockDependencies,
			"address",
		)
		assert.EqualError(t, err, "cannot resolve hostname 'address': random-failure")
	})

	t.Run("Address does not resolve", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		mockDependencies.On("LookupIP", "address").Return([]net.IP{}, nil)
		err := CheckHostnameAddress(
			mockDependencies,
			"address",
		)
		assert.EqualError(t, err, "cannot resolve hostname 'address'")
	})

	t.Run("Address resolve with localhost", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		mockDependencies.On("LookupIP", "address").Return(
			[]net.IP{net.IPv4(127,0,0,1)},
			nil,
		)
		err := CheckHostnameAddress(
			mockDependencies,
			"address",
		)
		assert.EqualError(t, err, "hostname 'address' is resolving with loopback address, should resolve with LAN address")
	})

	t.Run("Address resolve with LAN", func(t *testing.T) {
		mockDependencies := new(mockedNetworkInterfaces)
		mockDependencies.On("LookupIP", "address").Return([]net.IP{net.IPv4(1,1,1,1)},nil)
		err := CheckHostnameAddress(
			mockDependencies,
			"address",
		)
		assert.NoError(t, err)
	})
}

func TestSetup_openClusterCredential(t *testing.T) {
	t.Parallel()

	t.Run("File doesn't exists", func(t *testing.T) {
		nonExistingFile := test.GenerateRandomFile("File doesn't exists")
		assert.NoError(t, os.Remove(nonExistingFile.Name()))
		_, err := OpenClusterCredential(nonExistingFile.Name())
		assert.EqualError(
			t,
			err,
			fmt.Sprintf(
				"cannot find Cluster credential in %s, please copy the file from the existing server",
				nonExistingFile.Name(),
			),
		)
	})

	t.Run("File exists", func(t *testing.T) {
		existingFile := test.GenerateRandomFile("File exists")
		defer os.Remove(existingFile.Name())
		_, err := OpenClusterCredential(existingFile.Name())
		assert.NoError(t, err)
	})
}

func TestSaveBindAddressConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("Should check that the file is correctly written", func(t *testing.T) {
		actualResult := test.GenerateRandomFile("Should check that the file is correctly written")
		defer os.Remove(actualResult.Name())

		assert.NoError(t, SaveBindAddressConfiguration(actualResult.Name(), "127.0.0.1"))
		actualResultContent, err := ioutil.ReadFile(actualResult.Name())
		assert.NoError(t, err)
		assert.Equal(t, `{
  "bind_addr": "127.0.0.1"
}`, string(actualResultContent))
	})

	t.Run("Doesn't write network mask", func(t *testing.T) {
		actualResult := test.GenerateRandomFile("Should check that the file is correctly written")
		defer os.Remove(actualResult.Name())

		assert.NoError(t, SaveBindAddressConfiguration(actualResult.Name(), "127.0.0.1/24"))
		actualResultContent, err := ioutil.ReadFile(actualResult.Name())
		assert.NoError(t, err)
		assert.Equal(t, `{
  "bind_addr": "127.0.0.1"
}`, string(actualResultContent))
	})
}
