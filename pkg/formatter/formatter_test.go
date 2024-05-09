// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package formatter

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"
)

type validFormatter struct {
	Message string `json:"message"`
}

func (v *validFormatter) PlainRender() (string, error) {
	return v.Message, nil
}

func (v *validFormatter) JSONRender() (string, error) {
	return DefaultJSONRender(v)
}

type jsonOnlyFormatter struct {
	Message string `json:"message"`
}

func (v *jsonOnlyFormatter) PlainRender() (string, error) {
	return "", errors.New("Plain render not supported")
}

func (v *jsonOnlyFormatter) JSONRender() (string, error) {
	return DefaultJSONRender(v)
}

type plainOnlyFormatter struct {
	Message string
}

func (v *plainOnlyFormatter) PlainRender() (string, error) {
	return v.Message, nil
}

func (v *plainOnlyFormatter) JSONRender() (string, error) {
	return "", errors.New("Cannot encode to Json")
}

func TestRender(t *testing.T) {
	t.Parallel()

	type args struct {
		formatter  Formatter
		outputType OutputFormat
	}

	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			"Plain formatter of a valid struct",
			args{
				&validFormatter{Message: "test"},
				PlainFormatOutput,
			},
			"test",
			false,
		},
		{
			"Json formatter of a valid struct",
			args{
				&validFormatter{Message: "test"},
				JSONFormatOutput,
			},
			`{"message":"test"}`,
			false,
		},
		{
			"Plain formatter only error on Json formatting",
			args{
				&plainOnlyFormatter{Message: "test"},
				JSONFormatOutput,
			},
			"",
			true,
		},
		{
			"Json formatter only error on Plain formatting",
			args{
				&jsonOnlyFormatter{Message: "test"},
				PlainFormatOutput,
			},
			"",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.args.formatter, tt.args.outputType)
			if (err != nil) != tt.wantErr {
				t.Errorf("Render() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("Render() got = %v, want %v", got, tt.want)
			}
		})
	}
}

// This example is for documentation purposes https://blog.golang.org/examples
func ExampleRender() {
	// My structure, validFormatter, implements PlainRender() and JSONRender()
	dataToOutput := &validFormatter{Message: "hello world"}
	// Now I want to print the output in plain text
	plainRes, _ := Render(dataToOutput, PlainFormatOutput)
	fmt.Println(plainRes)

	// Now I want to print the output encoded in Json
	jsonRes, _ := Render(dataToOutput, JSONFormatOutput)
	fmt.Println(jsonRes)

	// Output:
	// hello world
	// {"message":"hello world"}
}
