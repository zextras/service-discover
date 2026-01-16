// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/zextras/service-discover/pkg/formatter"
)

const ConsulMutableConfigFile = "/etc/zextras/service-discover/config.json"

const configErrorFormat = "%w %s: %w"

// Config error definitions.
var (
	ErrConfigReadFailed  = errors.New("unable to read config file")
	ErrConfigWriteFailed = errors.New("unable to write config file")
	ErrConfigUnknown     = errors.New("unknown configuration")
)

type MutableConsulConfig struct {
	BindAddress string `json:"bind_addr"`
}

type Config struct {
	Command `kong:"-"`

	writer    io.Writer `kong:"-"`
	agentName string    `kong:"-"`

	Get  GetConfig  `cmd:"" help:"Get a specific configuration"`
	Set  SetConfig  `cmd:"" help:"Set a specific configuration"`
	List ListConfig `cmd:"" help:"List available configurations"`
}

func (v *Config) Run(_ *GlobalCommonFlags) error {
	return nil
}

type GetConfig struct {
	Command `kong:"-"`

	ReadFile  func(filename string) ([]byte, error) `kong:"-"`
	writer    io.Writer                             `kong:"-"`
	agentName string                                `kong:"-"`
	Config    string                                `arg:"" required:"" name:"config" help:"Config to get."`
}

type getConfigOutput struct {
	BindAddress string `json:"bind-address"`
}

func (o *getConfigOutput) PlainRender() (string, error) {
	var out = ""
	if o.BindAddress != "" {
		out += o.BindAddress + "\n"
	}

	return out, nil
}

func (o *getConfigOutput) JSONRender() (string, error) {
	return formatter.DefaultJSONRender(o)
}

func (v *GetConfig) Run(globalFlags *GlobalCommonFlags) error {
	data, err := v.ReadFile(ConsulMutableConfigFile)
	if err != nil {
		return fmt.Errorf(configErrorFormat, ErrConfigReadFailed, ConsulMutableConfigFile, err)
	}

	config := MutableConsulConfig{}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return err
	}

	output := &getConfigOutput{}

	switch v.Config {
	case "bind-address":
		output.BindAddress = config.BindAddress
	default:
		return fmt.Errorf("%w: '%s'", ErrConfigUnknown, v.Config)
	}

	out, err := formatter.Render(output, globalFlags.Format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(v.writer, out)

	return err
}

type SetConfig struct {
	Command `kong:"-"`

	ReadFile  func(filename string) ([]byte, error)                      `kong:"-"`
	WriteFile func(filename string, data []byte, perm fs.FileMode) error `kong:"-"`
	writer    io.Writer                                                  `kong:"-"`
	agentName string                                                     `kong:"-"`
	Config    string                                                     `arg:"" required:"" help:"Config to set."`
	Value     string                                                     `arg:"" required:"" help:"Config value."`
}

func (v *SetConfig) Run(_ *GlobalCommonFlags) error {
	data, err := v.ReadFile(ConsulMutableConfigFile)
	if err != nil {
		return fmt.Errorf(configErrorFormat, ErrConfigReadFailed, ConsulMutableConfigFile, err)
	}

	config := MutableConsulConfig{}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return err
	}

	switch v.Config {
	case "bind-address":
		config.BindAddress = v.Value
	default:
		return fmt.Errorf("%w: '%s'", ErrConfigUnknown, v.Config)
	}

	data, err = json.MarshalIndent(&config, "", "  ")
	if err != nil {
		return err
	}

	err = v.WriteFile(ConsulMutableConfigFile, data, 0600)
	if err != nil {
		return fmt.Errorf(configErrorFormat, ErrConfigWriteFailed, ConsulMutableConfigFile, err)
	}

	return nil
}

type ListConfig struct {
	Command `kong:"-"`

	writer    io.Writer `kong:"-"`
	agentName string    `kong:"-"`
}

type listConfigOutput struct {
	configs []string
}

func (o *listConfigOutput) PlainRender() (string, error) {
	var outSb strings.Builder

	for _, config := range o.configs {
		outSb.WriteString(config + "\n")
	}

	return outSb.String(), nil
}

func (o *listConfigOutput) JSONRender() (string, error) {
	out, err := json.MarshalIndent(o.configs, "", "  ")

	return string(out), err
}

func (v *ListConfig) Run(globalFlags *GlobalCommonFlags) error {
	output := listConfigOutput{
		configs: []string{"bind-address"},
	}

	out, err := formatter.Render(&output, globalFlags.Format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(v.writer, out)

	return err
}
