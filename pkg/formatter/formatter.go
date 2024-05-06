// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

// Package formatter allows to render strings meant to be output to the final user. Currently the supported outputs are
// plain and JSON. Once you implement the Formatter interface, you are free to use Render and pass the desired
// OutputFormat. The output will be saved on a string, so you will be able to perform additional manipulation before
// printing out the TTY, or you have the possibility to execute tests without any side effects.
package formatter

import (
	"encoding/json"

	"github.com/pkg/errors"
)

type OutputFormat int

const (
	PlainFormatOutput OutputFormat = iota
	JsonFormatOutput
)

// The Formatter interface defines the currently supported encoding mechanisms. Currently, they are subdivided as
// follows:
//
// - PlainRender() is intended to be used when the final reader will be a human
//
// - JsonRender() is intended to be used when the final reader will be another program. Keep in mind that the output of
// this format could very well be the input of another one.
//
// Every struct implementing this interface will have to provide valid implementations. Error are also considered, in
// case it is not possible to format a specific output to a desired version.
type Formatter interface {
	// PlainRender renders human-readable output
	PlainRender() (string, error)
	// JsonRender renders json output, that is more suitable as input of other programs
	JsonRender() (string, error)
}

type EmptyFormatter struct{}

func (e EmptyFormatter) PlainRender() (string, error) {
	return "", nil
}

func (e EmptyFormatter) JsonRender() (string, error) {
	return "{}", nil
}

// DefaultJSONRender wrappers a call to json.Marshal function, saving up time and keeping your code DRY.
func DefaultJSONRender(v any) (string, error) {
	bytes, err := json.Marshal(v)
	return string(bytes), err
}

// Render function will format the passed Formatter to the desired OutputFormat.
func Render(formatter Formatter, outputType OutputFormat) (string, error) {
	switch outputType {
	case 0:
		return formatter.PlainRender()
	case 1:
		return formatter.JsonRender()
	default:
		return "", errors.New("The passed formatting option is not valid")
	}
}
