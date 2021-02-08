package command

import (
	"bitbucket.org/zextras/service-discover/cli/lib/formatter"
	"bytes"
	"github.com/stretchr/testify/assert"
	"io"
	"io/ioutil"
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
			byteOut, err := ioutil.ReadAll(&buffer)
			assert.Nil(t, err)
			assert.Equal(t, tt.assertion, string(byteOut))
		})
	}
}
