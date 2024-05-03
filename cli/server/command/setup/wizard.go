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

package setup

import (
	"os"

	"github.com/Zextras/service-discover/cli/lib/command"
	"github.com/Zextras/service-discover/cli/lib/formatter"
	"github.com/Zextras/service-discover/cli/lib/term"
	"github.com/pkg/errors"
)

// NewWizardSetup is just a wrapper over the original setup, that performs interactive UI experience
func NewWizardSetup(setup *Setup) Wizard {
	return Wizard{
		originalSetup: setup,
		Password:      "",
		BindAddress:   "",
		FirstInstance: false,
	}
}

// Wizard si a wrap of the standard setup procedure that includes interactive setup
type Wizard struct {
	originalSetup *Setup `kong:"-"`
	Password      string `help:"Set a custom password for the encrypted secret files. If none is set, a random one will be generated and printed"`
	BindAddress   string `arg optional help:"The binding address to bind service-discoverd daemon"`
	FirstInstance bool   `optional default:"false" help:"Force the setup to behave as this was the first server setup"`
}

func (s *Wizard) Run(commonFlags *command.GlobalCommonFlags) error {
	ui, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return err
	}

	defer ui.Close()
	d := realDependencies{
		ui: &ui,
	}

	err = preRun(d)
	if err != nil {
		return err
	}

	if commonFlags.Format != formatter.PlainFormatOutput {
		return errors.New("only plain formatting is supported when in wizard mode")
	}

	//if manually specified do not check it
	if !s.FirstInstance {
		s.FirstInstance, err = s.originalSetup.isFirstInstance(d)
		if err != nil {
			return err
		}
	}

	if s.FirstInstance {
		term.MustWrite(ui.WriteString("Setup of first service-discover server instance\r\n"))
	} else {
		term.MustWrite(ui.WriteString("Setup of secondary service-discover server instance\r\n"))
	}

	inputs, err := gatherInputs(d, s.FirstInstance)
	if err != nil {
		return err
	}

	s.Password = inputs.Password
	s.BindAddress = inputs.BindAddress

	// We fill the original setup here
	s.originalSetup.Password = s.Password
	s.originalSetup.BindAddress = s.BindAddress
	s.originalSetup.FirstInstance = s.FirstInstance

	if s.FirstInstance {
		_, err = s.originalSetup.firstSetup(d)
	} else {
		_, err = s.originalSetup.importSetup(d)
	}

	if err != nil {
		return err
	}

	return nil
}
