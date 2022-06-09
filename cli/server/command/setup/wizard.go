package setup

import (
	"bitbucket.org/zextras/service-discover/cli/lib/command"
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bitbucket.org/zextras/service-discover/cli/lib/term"
	"github.com/pkg/errors"
	"os"
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
