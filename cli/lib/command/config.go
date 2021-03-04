package command

import (
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

const ConsulMutableConfigFile = "/etc/zextras/service-discover/config.json"

type MutableConsulConfig struct {
	BindAddress string `json:"bind_addr"`
}

type Config struct {
	Command   `kong:"-"`
	writer    io.Writer `kong:"-"`
	agentName string    `kong:"-"`

	Get  GetConfig  `cmd help:"Get a specific configuration"`
	Set  SetConfig  `cmd help:"Set a specific configuration"`
	List ListConfig `cmd help:"List available configurations"`
}

func (v *Config) Run(_globalFlags *GlobalCommonFlags) error {
	return nil
}

type GetConfig struct {
	Command   `kong:"-"`
	ReadFile  func(filename string) ([]byte, error) `kong:"-"`
	writer    io.Writer                             `kong:"-"`
	agentName string                                `kong:"-"`
	Config    string                                `arg required name:"config" help:"Config to get."`
}

type getConfigOutput struct {
	BindAddress string `json:"bind-address"`
}

func (o *getConfigOutput) PlainRender() (string, error) {
	var out = ""
	if len(o.BindAddress) > 0 {
		out += fmt.Sprintf("%s\n", o.BindAddress)
	}
	return out, nil
}

func (o *getConfigOutput) JsonRender() (string, error) {
	return formatter.DefaultJsonRender(o)
}

func (v *GetConfig) Run(globalFlags *GlobalCommonFlags) error {
	data, err := v.ReadFile(ConsulMutableConfigFile)
	if err != nil {
		return errors.New("unable to read " + ConsulMutableConfigFile + ": " + err.Error())
	}

	config := MutableConsulConfig{}
	err = json.Unmarshal(data, &config)
	output := &getConfigOutput{}

	switch v.Config {
	case "bind-address":
		output.BindAddress = config.BindAddress
		break
	default:
		return errors.New("unknown configuration '" + v.Config + "'")
	}
	out, err := formatter.Render(output, globalFlags.Format)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(v.writer, out)
	return err
}

type SetConfig struct {
	Command   `kong:"-"`
	ReadFile  func(filename string) ([]byte, error)                      `kong:"-"`
	WriteFile func(filename string, data []byte, perm fs.FileMode) error `kong:"-"`
	writer    io.Writer                                                  `kong:"-"`
	agentName string                                                     `kong:"-"`
	Config    string                                                     `arg required help:"Config to set."`
	Value     string                                                     `arg required help:"Config value."`
}

func (v *SetConfig) Run(_globalFlags *GlobalCommonFlags) error {
	data, err := v.ReadFile(ConsulMutableConfigFile)
	if err != nil {
		return errors.New("unable to read " + ConsulMutableConfigFile + ": " + err.Error())
	}

	config := MutableConsulConfig{}
	err = json.Unmarshal(data, &config)

	switch v.Config {
	case "bind-address":
		config.BindAddress = v.Value
		break
	default:
		return errors.New("unknown configuration '" + v.Config + "'")
	}

	data, _ = json.Marshal(&config)
	err = v.WriteFile(ConsulMutableConfigFile, data, 0644)
	if err != nil {
		return errors.New("unable to write " + ConsulMutableConfigFile + ": " + err.Error())
	}

	return err
}

type ListConfig struct {
	Command   `kong:"-"`
	writer    io.Writer `kong:"-"`
	agentName string    `kong:"-"`
}

type listConfigOutput struct {
	configs []string
}

func (o *listConfigOutput) PlainRender() (string, error) {
	out := ""
	for _, config := range o.configs {
		out += config + "\n"
	}
	return out, nil
}

func (o *listConfigOutput) JsonRender() (string, error) {
	out, err := json.Marshal(o.configs)
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
	_, err = fmt.Fprintf(v.writer, out)
	return err
}
