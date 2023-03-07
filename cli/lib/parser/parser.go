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

// Package parser provides an abstraction layer over the parser engine. This package allows and integrates possible
// plugin and additional components over Kong, that is the real engine that parses the input arguments.
package parser

import (
	"github.com/alecthomas/kong"
	"github.com/pkg/errors"
	"reflect"
	"strings"
)

const formatType = "format"
const formatValueToken kong.TokenType = iota

// A format struct allows to create a dummy struct that implements Kong.Mapper interface.
type format struct{}

// Decode will decode the incoming flag format and understands if it follows the accepted values, "plain" and "json".
func (format) Decode(ctx *kong.DecodeContext, target reflect.Value) error {
	if ctx.Scan.Peek().Type == formatValueToken {
		token := ctx.Scan.Pop()
		switch value := token.Value.(type) {
		case string:
			// We know if implements interface string, we can treat it as one
			value = strings.ToLower(value)
			switch value {
			case "plain":
				target.SetInt(0)
			case "json":
				target.SetInt(1)
			default:
				return errors.Errorf("formatType value must be plain or json, but got %q", value)
			}
		default:
			return errors.Errorf("expected string but got %q (%T)", token.Value, token.Value)
		}
	}
	return nil
}

// Parse will add to the underlying Kong engine our custom plugin and extensions, and then it will call Kong's Parse
// function and will return its result.
func Parse(cli interface{}, options ...kong.Option) *kong.Context {
	formatModule := kong.NamedMapper(formatType, &format{})
	return kong.Parse(cli, append(options, formatModule)...)
}
