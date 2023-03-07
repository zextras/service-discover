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
	"github.com/Zextras/service-discover/cli/lib/formatter"
	"github.com/stretchr/testify/assert"
	"io"
	"testing"
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
			fields{&buffer, "0.1.0", "service-discover", "agent"},
			args{
				globalFlags: &GlobalCommonFlags{Format: formatter.PlainFormatOutput},
			},
			`service-discover version: 0.1.0
agent version: N/A
`,
		},
		{
			"Json output without available agent",
			fields{&buffer, "0.1.0", "service-discover", "agent"},
			args{
				globalFlags: &GlobalCommonFlags{Format: formatter.JsonFormatOutput},
			},
			`{"cli_version":"0.1.0","agent_version":"N/A"}`,
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
