// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"os"

	"github.com/pkg/errors"
	"github.com/zextras/service-discover/pkg/command"
	"github.com/zextras/service-discover/pkg/formatter"
	"github.com/zextras/service-discover/pkg/term"
)

// Wizard is a wrap of the standard setup procedure that includes interactive setup.
type Wizard struct {
	originalSetup *Setup

	Password    string `help:"Custom password for encrypted secret files. If unset, one is generated"`
	BindAddress string `arg:"" optional:"" help:"The binding address to bind service-discoverd daemon"`
}

// NewWizardSetup is a wrapper over the original setup that performs interactive UI experience.
func NewWizardSetup(setup *Setup) Wizard {
	return Wizard{
		originalSetup: setup,
	}
}

func (s *Wizard) Run(commonFlags *command.GlobalCommonFlags) error {
	userInterface, err := term.New(os.Stdin, os.Stdout, term.DefaultTermPrompt)
	if err != nil {
		return err
	}

	defer userInterface.Close()

	dependency := realDependencies{
		ui: &userInterface,
	}

	err = preRun(s.originalSetup.ClusterCredential, &dependency)
	if err != nil {
		return err
	}

	if commonFlags.Format != formatter.PlainFormatOutput {
		return errors.New("only plain formatting is supported when in wizard mode")
	}

	inputs, err := gatherInputs(dependency)
	if err != nil {
		return err
	}

	s.Password = inputs.Password
	s.BindAddress = inputs.BindAddress

	// We fill the original setup here
	s.originalSetup.Password = s.Password
	s.originalSetup.BindAddress = s.BindAddress

	_, err = s.originalSetup.setup(&dependency)
	if err != nil {
		return err
	}

	return nil
}
