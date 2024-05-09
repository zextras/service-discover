// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/encrypter"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/term"
)

const (
	SetupConsulToken = "SETUP_CONSUL_TOKEN" // #nosec
)

type BootstrapToken struct {
	termUIProvider                term.UIProvider
	clusterCredentialFileLocation string `kong:"-"`
	Command                       `kong:"-"`
	writer                        io.Writer `kong:"-"`
	agentName                     string    `kong:"-"`
	Setup                         bool      `optional name:"setup" help:"Used in setup scripts, doesn't prompt anything and returns $SETUP_CONSUL_TOKEN if defined."`
	Password                      string    `optional name:"password" help:"feed bootstrap password"`
}

type outputBootstrapToken struct {
	Token string `json:"token"`
}

func (o *outputBootstrapToken) PlainRender() (string, error) {
	return o.Token, nil
}

func (o *outputBootstrapToken) JSONRender() (string, error) {
	return formatter.DefaultJSONRender(o)
}

type OutputWrapper struct {
	writer *os.File
}

func (o OutputWrapper) Write(buffer []byte) (n int, err error) {
	replaced := bytes.ReplaceAll(buffer, []byte("\r\n"), []byte(""))
	n, err = o.writer.Write(replaced)
	// report \r\n as written
	n += len(buffer) - len(replaced)
	return
}

func (v *BootstrapToken) ReadToken() (string, error) {
	var wrapper io.Writer
	if v.Setup {
		// avoid printing "\r\n token"
		wrapper = OutputWrapper{os.Stdout}
	} else {
		wrapper = os.Stdout
	}

	prompt := "Insert the cluster credential password: "
	if v.Setup {
		prompt = ""
	}

	var password string

	if v.Password == "" {
		var err error

		ui, err := v.termUIProvider.Get(wrapper)
		if err != nil {
			return "", err
		}

		defer func(ui term.Terminal) {
			_ = ui.Close()
		}(ui)

		password, err = ui.ReadPassword(prompt)
		if err != nil {
			switch err.(type) {
			case term.NotATerminalError:
				password = term.MustRead(ui.ReadLine())
			default:
				return "", err
			}
		}
	} else {
		password = v.Password
	}

	clusterCredentialFile, err := OpenClusterCredential(v.clusterCredentialFileLocation)
	if err != nil {
		return "", errors.Errorf("unable to open %s: %s", v.clusterCredentialFileLocation, err)
	}

	defer func(clusterCredentialFile *os.File) {
		_ = clusterCredentialFile.Close()
	}(clusterCredentialFile)

	credReader, err := encrypter.NewReader(clusterCredentialFile, []byte(password))
	if err != nil {
		return "", err
	}

	extractedFiles, err := encrypter.ReadFiles(credReader, ConsulACLBootstrap)
	if err != nil {
		return "", err
	}

	aclBootstrapToken := ACLTokenCreation{}
	if err := json.Unmarshal(extractedFiles[ConsulACLBootstrap], &aclBootstrapToken); err != nil {
		return "", errors.WithMessagef(err, "unable to decode ACL Bootstrap token")
	}

	return aclBootstrapToken.SecretID, nil
}

func (v *BootstrapToken) Run(globalFlags *GlobalCommonFlags) error {
	token, present := os.LookupEnv(SetupConsulToken)
	if !v.Setup || !present || len(token) == 0 {
		var err error

		token, err = v.ReadToken()
		if err != nil {
			return err
		}
	}

	res := &outputBootstrapToken{
		token,
	}

	out, err := formatter.Render(res, globalFlags.Format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(v.writer, out)

	return err
}
