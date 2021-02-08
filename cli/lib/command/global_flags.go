package command

import (
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
)

// The GlobalCommonFlags struct represents flags that applies globally to the whole application.
type GlobalCommonFlags struct {
	Format formatter.OutputFormat `help:"Format output in plain or json" type:"format"`
}
