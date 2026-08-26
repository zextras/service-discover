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

var (
	errUnableToRead  = errors.New("unable to read " + ConsulMutableConfigFile)
	errUnableToWrite = errors.New("unable to write " + ConsulMutableConfigFile)
	errUnknownConfig = errors.New("unknown configuration")
)

const (
	ConsulMutableConfigFile = "/etc/zextras/service-discover/config.json"
	configBindAddress       = "bind-address"
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
		return fmt.Errorf("%w: %w", errUnableToRead, err)
	}

	config := MutableConsulConfig{}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return err
	}

	output := &getConfigOutput{}

	switch v.Config {
	case configBindAddress:
		output.BindAddress = config.BindAddress
	default:
		return fmt.Errorf("%w: %s", errUnknownConfig, v.Config)
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
		return fmt.Errorf("%w: %w", errUnableToRead, err)
	}

	config := MutableConsulConfig{}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return err
	}

	switch v.Config {
	case configBindAddress:
		config.BindAddress = v.Value
	default:
		return fmt.Errorf("%w: %s", errUnknownConfig, v.Config)
	}

	data, err = json.MarshalIndent(&config, "", "  ")
	if err != nil {
		return err
	}

	err = v.WriteFile(ConsulMutableConfigFile, data, 0644)
	if err != nil {
		return fmt.Errorf("%w: %w", errUnableToWrite, err)
	}

	return err
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
	var builder strings.Builder
	for _, config := range o.configs {
		builder.WriteString(config)
		builder.WriteByte('\n')
	}

	return builder.String(), nil
}

func (o *listConfigOutput) JSONRender() (string, error) {
	out, err := json.MarshalIndent(o.configs, "", "  ")

	return string(out), err
}

func (v *ListConfig) Run(globalFlags *GlobalCommonFlags) error {
	output := listConfigOutput{
		configs: []string{configBindAddress},
	}

	out, err := formatter.Render(&output, globalFlags.Format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(v.writer, out)

	return err
}
