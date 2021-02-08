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
