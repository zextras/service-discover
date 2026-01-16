// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"os"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	sharedsetup "github.com/zextras/service-discover/pkg/command/setup"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/term"
)

// Wizard is a wrap of the standard setup procedure that includes interactive setup.
type Wizard struct {
	originalSetup *Setup `kong:"-"`
	Password      string `help:"Custom password for encrypted secret files. If unset, one is generated"`
	BindAddress   string `arg:"" optional:"" help:"The binding address to bind service-discoverd daemon"`
	FirstInstance bool   `optional:"" default:"false" help:"Force the setup to behave as first server setup"`
}

// NewWizardSetup is a wrapper over the original setup that performs interactive UI experience.
func NewWizardSetup(setup *Setup) Wizard {
	return Wizard{
		originalSetup: setup,
		Password:      "",
		BindAddress:   "",
		FirstInstance: false,
	}
}

func (s *Wizard) Run(commonFlags *command.GlobalCommonFlags) error {
	userInterface, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return err
	}

	defer userInterface.Close()

	deps := sharedsetup.RealDependencies{
		UI: &userInterface,
	}

	err = preRun(deps)
	if err != nil {
		return err
	}

	if commonFlags.Format != formatter.PlainFormatOutput {
		return errors.New("only plain formatting is supported when in wizard mode")
	}

	// if manually specified do not check it
	if !s.FirstInstance {
		s.FirstInstance, err = s.originalSetup.isFirstInstance(deps)
		if err != nil {
			return err
		}
	}

	if s.FirstInstance {
		term.MustWrite(userInterface.WriteString("Setup of first service-discover server instance\r\n"))
	} else {
		term.MustWrite(userInterface.WriteString("Setup of secondary service-discover server instance\r\n"))
	}

	inputs, err := gatherInputs(deps, s.FirstInstance)
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
		_, err = s.originalSetup.firstSetup(deps)
	} else {
		_, err = s.originalSetup.importSetup(deps)
	}

	if err != nil {
		return err
	}

	return nil
}
