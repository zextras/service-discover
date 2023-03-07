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

// DefaultJsonRender wrappers a call to json.Marshal function, saving up time and keeping your code DRY.
func DefaultJsonRender(v interface{}) (string, error) {
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
