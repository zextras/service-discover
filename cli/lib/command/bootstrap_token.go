/*
 * Copyright (C) 2023 Zextras srl
 *
 *     This program is free software: you can redistribute it and/or modify
 *     it under the terms of the GNU Affero General Public License as published by
 *     the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU Affero General Public License for more details.
 *
 *     You should have received a copy of the GNU Affero General Public License
 *     along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 */

package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Zextras/service-discover/cli/lib/credentialsEncrypter"
	"github.com/Zextras/service-discover/cli/lib/formatter"
	"github.com/Zextras/service-discover/cli/lib/term"
	"github.com/pkg/errors"
	"io"
	"os"
)

const (
	ConsulConfig      = "/etc/zextras/service-discover"
	ClusterCredential = ConsulConfig + "/cluster-credentials.tar.gpg"
	SetupConsulToken  = "SETUP_CONSUL_TOKEN" // #nosec
)

type BootstrapToken struct {
	Command   `kong:"-"`
	writer    io.Writer `kong:"-"`
	agentName string    `kong:"-"`
	Setup     bool      `optional name:"setup" help:"Used in setup scripts, doesn't prompt anything and returns $SETUP_CONSUL_TOKEN if defined."`
	Password  string    `optional name:"password" help:"feed bootstrap password"`
}

type outputBootstrapToken struct {
	Token string `json:"token"`
}

func (o *outputBootstrapToken) PlainRender() (string, error) {
	return o.Token, nil
}

func (o *outputBootstrapToken) JsonRender() (string, error) {
	return formatter.DefaultJsonRender(o)
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

	ui, err := term.New(os.Stdin, wrapper, term.DefaultTermPrompt)
	if err != nil {
		return "", err
	}
	defer func(ui term.Terminal) {
		_ = ui.Close()
	}(ui)

	prompt := "Insert the cluster credential password: "
	if v.Setup {
		prompt = ""
	}
	password := ""
	if v.Password == "" {
		password, err = ui.ReadPassword(prompt)
	} else {
		password = v.Password
	}
	if err != nil {
		switch err.(type) {
		case term.NotATerminalError:
			password = term.MustRead(ui.ReadLine())
		default:
			return "", err
		}
	}

	clusterCredentialFile, err := OpenClusterCredential(ClusterCredential)
	if err != nil {
		return "", errors.New(fmt.Sprintf("unable to open %s: %s", ClusterCredential, err))
	}
	defer func(clusterCredentialFile *os.File) {
		_ = clusterCredentialFile.Close()
	}(clusterCredentialFile)
	credReader, err := credentialsEncrypter.NewReader(clusterCredentialFile, []byte(password))
	if err != nil {
		return "", err
	}

	extractedFiles, err := credentialsEncrypter.ReadFiles(credReader, ConsulAclBootstrap)
	if err != nil {
		return "", err
	}

	aclBootstrapToken := ACLTokenCreation{}
	if err := json.Unmarshal(extractedFiles[ConsulAclBootstrap], &aclBootstrapToken); err != nil {
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
