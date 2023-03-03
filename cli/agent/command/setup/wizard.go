package setup

import (
	"github.com/Zextras/service-discover/cli/lib/command"
	"github.com/Zextras/service-discover/cli/lib/formatter"
	"github.com/Zextras/service-discover/cli/lib/term"
	"github.com/pkg/errors"
	"os"
)

// NewWizardSetup is just a wrapper over the original setup, that performs interactive UI experience
func NewWizardSetup(setup *Setup) Wizard {
	return Wizard{
		originalSetup: setup,
	}
}

// Wizard si a wrap of the standard setup procedure that includes interactive setup
type Wizard struct {
	originalSetup *Setup

	Password    string `help:"Set a custom password for the encrypted secret files. If none is set, a random one will be generated and printed"`
	BindAddress string `arg optional help:"The binding address to bind service-discoverd daemon"`
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

	err = preRun(s.originalSetup.ClusterCredential, &d)
	if err != nil {
		return err
	}

	if commonFlags.Format != formatter.PlainFormatOutput {
		return errors.New("only plain formatting is supported when in wizard mode")
	}
	inputs, err := gatherInputs(d)
	if err != nil {
		return err
	}

	s.Password = inputs.Password
	s.BindAddress = inputs.BindAddress

	// We fill the original setup here
	s.originalSetup.Password = s.Password
	s.originalSetup.BindAddress = s.BindAddress

	_, err = s.originalSetup.setup(&d)
	if err != nil {
		return err
	}

	return nil
}
