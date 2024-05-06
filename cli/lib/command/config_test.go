// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"bytes"
	"errors"
	"io/fs"
	"testing"

	"github.com/Zextras/service-discover/cli/lib/formatter"
	"github.com/stretchr/testify/assert"
)

func TestListConfig(t *testing.T) {
	t.Run("list plain", func(t *testing.T) {
		buffer := new(bytes.Buffer)
		cmd := ListConfig{
			Command:   Command{},
			writer:    buffer,
			agentName: "",
		}
		flags := GlobalCommonFlags{
			Format: formatter.PlainFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.Nil(t, err)
		assert.Equal(t, "bind-address\n", buffer.String())
	})

	t.Run("list json", func(t *testing.T) {
		buffer := new(bytes.Buffer)
		cmd := ListConfig{
			Command:   Command{},
			writer:    buffer,
			agentName: "",
		}
		flags := GlobalCommonFlags{
			Format: formatter.JsonFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.Nil(t, err)
		assert.Equal(t, `[
  "bind-address"
]`, buffer.String())
	})
}

func TestGetConfig(t *testing.T) {
	t.Run("get plain", func(t *testing.T) {
		buffer := new(bytes.Buffer)
		cmd := GetConfig{
			Command: Command{},
			ReadFile: func(filename string) ([]byte, error) {
				assert.Equal(t, "/etc/zextras/service-discover/config.json", filename)
				return []byte("{ \"bind_addr\": \"192.168.0.1\" }"), nil
			},
			writer: buffer,
			Config: "bind-address",
		}
		flags := GlobalCommonFlags{
			Format: formatter.PlainFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.Nil(t, err)
		assert.Equal(t, "192.168.0.1\n", buffer.String())
	})

	t.Run("get json", func(t *testing.T) {
		buffer := new(bytes.Buffer)
		cmd := GetConfig{
			Command: Command{},
			ReadFile: func(filename string) ([]byte, error) {
				assert.Equal(t, "/etc/zextras/service-discover/config.json", filename)
				return []byte("{ \"bind_addr\": \"192.168.0.1\" }"), nil
			},
			writer: buffer,
			Config: "bind-address",
		}
		flags := GlobalCommonFlags{
			Format: formatter.JsonFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.Nil(t, err)
		assert.Equal(t, "{\"bind-address\":\"192.168.0.1\"}", buffer.String())
	})

	t.Run("get plain unknown config", func(t *testing.T) {
		buffer := new(bytes.Buffer)
		cmd := GetConfig{
			Command: Command{},
			ReadFile: func(filename string) ([]byte, error) {
				assert.Equal(t, "/etc/zextras/service-discover/config.json", filename)
				return []byte("{ \"bind_addr\": \"192.168.0.1\" }"), nil
			},
			writer: buffer,
			Config: "random-name",
		}
		flags := GlobalCommonFlags{
			Format: formatter.PlainFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.NotNil(t, err)
		assert.Equal(t, "unknown configuration 'random-name'", err.Error())
		assert.Equal(t, "", buffer.String())
	})

	t.Run("get cannot read", func(t *testing.T) {
		buffer := new(bytes.Buffer)
		cmd := GetConfig{
			Command: Command{},
			ReadFile: func(filename string) ([]byte, error) {
				return nil, errors.New("fake error")
			},
			writer: buffer,
			Config: "bind-address",
		}
		flags := GlobalCommonFlags{
			Format: formatter.PlainFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.NotNil(t, err)
		assert.Equal(t, "unable to read /etc/zextras/service-discover/config.json: fake error", err.Error())
	})
}

func TestSetConfig(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		consoleOutput := new(bytes.Buffer)
		configFileOutput := new(bytes.Buffer)
		cmd := SetConfig{
			Command: Command{},
			ReadFile: func(filename string) ([]byte, error) {
				assert.Equal(t, "/etc/zextras/service-discover/config.json", filename)
				return []byte("{ \"bind_addr\": \"192.168.0.1\" }"), nil
			},
			WriteFile: func(filename string, data []byte, perm fs.FileMode) error {
				assert.Equal(t, "/etc/zextras/service-discover/config.json", filename)
				configFileOutput.Write(data)
				return nil
			},
			writer: consoleOutput,
			Config: "bind-address",
			Value:  "10.0.0.1",
		}
		flags := GlobalCommonFlags{
			Format: formatter.PlainFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.Nil(t, err)
		assert.Equal(t, "", consoleOutput.String())
		assert.Equal(t, `{
  "bind_addr": "10.0.0.1"
}`, configFileOutput.String())
	})

	t.Run("set unknown config", func(t *testing.T) {
		consoleOutput := new(bytes.Buffer)
		cmd := SetConfig{
			Command: Command{},
			ReadFile: func(filename string) ([]byte, error) {
				assert.Equal(t, "/etc/zextras/service-discover/config.json", filename)
				return []byte("{ \"bind_addr\": \"192.168.0.1\" }"), nil
			},
			WriteFile: func(filename string, data []byte, perm fs.FileMode) error {
				assert.Fail(t, "should never be called")
				return errors.New("test broken")
			},
			writer: consoleOutput,
			Config: "random-name",
			Value:  "10.0.0.1",
		}
		flags := GlobalCommonFlags{
			Format: formatter.PlainFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.NotNil(t, err)
		assert.Equal(t, "unknown configuration 'random-name'", err.Error())
		assert.Equal(t, "", consoleOutput.String())
	})

	t.Run("set read error", func(t *testing.T) {
		consoleOutput := new(bytes.Buffer)
		cmd := SetConfig{
			Command: Command{},
			ReadFile: func(filename string) ([]byte, error) {
				return nil, errors.New("fake error")
			},
			WriteFile: func(filename string, data []byte, perm fs.FileMode) error {
				assert.Fail(t, "should never be called")
				return errors.New("test broken")
			},
			writer: consoleOutput,
			Config: "bind-address",
			Value:  "10.0.0.1",
		}
		flags := GlobalCommonFlags{
			Format: formatter.PlainFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.NotNil(t, err)
		assert.Equal(t, "unable to read /etc/zextras/service-discover/config.json: fake error", err.Error())
		assert.Equal(t, "", consoleOutput.String())
	})

	t.Run("set write error", func(t *testing.T) {
		consoleOutput := new(bytes.Buffer)
		cmd := SetConfig{
			Command: Command{},
			ReadFile: func(filename string) ([]byte, error) {
				return []byte("{ \"bind_addr\": \"192.168.0.1\" }"), nil
			},
			WriteFile: func(filename string, data []byte, perm fs.FileMode) error {
				return errors.New("fake error")
			},
			writer: consoleOutput,
			Config: "bind-address",
			Value:  "10.0.0.1",
		}
		flags := GlobalCommonFlags{
			Format: formatter.PlainFormatOutput,
		}
		err := cmd.Run(&flags)
		assert.NotNil(t, err)
		assert.Equal(t, "unable to write /etc/zextras/service-discover/config.json: fake error", err.Error())
		assert.Equal(t, "", consoleOutput.String())
	})
}
