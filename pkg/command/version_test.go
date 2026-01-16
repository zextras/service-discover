// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package command

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zextras/service-discover/pkg/formatter"
)

func TestVersion_Run(t *testing.T) {
	t.Parallel()

	type fields struct {
		Writer     io.Writer
		CLIVersion string
		CLIName    string
		AgentName  string
	}

	type args struct {
		globalFlags *GlobalCommonFlags
	}

	buffer := bytes.Buffer{}
	tests := []struct {
		name      string
		fields    fields
		args      args
		assertion string
	}{
		{
			"Plain output without available agent",
			fields{&buffer, "0.2.3", "service-discover", "agent"},
			args{
				globalFlags: &GlobalCommonFlags{Format: formatter.PlainFormatOutput},
			},
			`service-discover version: 0.2.3
agent version: N/A
`,
		},
		{
			"Json output without available agent",
			fields{&buffer, "0.2.3", "service-discover", "agent"},
			args{
				globalFlags: &GlobalCommonFlags{Format: formatter.JSONFormatOutput},
			},
			`{"cli_version":"0.2.3","agent_version":"N/A"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &Version{
				Command: Command{
					applicationName:    tt.fields.CLIName,
					applicationVersion: tt.fields.CLIVersion,
				},
				writer:    tt.fields.Writer,
				agentName: tt.fields.AgentName,
			}
			assert.Nil(t, v.Run(tt.args.globalFlags))

			byteOut, err := io.ReadAll(&buffer)
			assert.Nil(t, err)
			assert.Equal(t, tt.assertion, string(byteOut))
		})
	}
}
