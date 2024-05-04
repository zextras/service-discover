// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"github.com/Zextras/service-discover/cli/lib/formatter"
)

// The GlobalCommonFlags struct represents flags that applies globally to the whole application.
type GlobalCommonFlags struct {
	Format formatter.OutputFormat `help:"Format output in plain or json" type:"format"`
}
