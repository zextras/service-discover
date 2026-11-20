// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mesh

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

const tokenAttribute = "token"

var (
	errNoService      = errors.New("no service definition found")
	errTokenDefined   = errors.New("the service definition already sets a token")
	errUnsupportedHCL = errors.New("unsupported service definition")

	// objectTokenKey matches a token key written inside an object style definition, `"token" = "..."`.
	objectTokenKey = regexp.MustCompile(`(?m)^\s*"?token"?\s*[=:]`)
)

// InjectToken returns the Consul service definition src with token set as the ACL token of every service it
// registers. Consul registers the service, its checks and its sidecar with that token. The package definitions
// carry no token and the comments and layout of the file are preserved.
//
// Both forms Consul accepts are supported: `service { ... }` and `services { ... }` blocks, and the object
// attribute `service = { ... }`.
func InjectToken(src []byte, name, token string) ([]byte, error) {
	file, diags := hclwrite.ParseConfig(src, name, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, fmt.Errorf("unable to parse %s: %w", name, diags)
	}

	body := file.Body()
	injected := 0

	for _, block := range body.Blocks() {
		if block.Type() != "service" && block.Type() != "services" {
			continue
		}

		block.Body().SetAttributeValue(tokenAttribute, cty.StringVal(token))

		injected++
	}

	for attrName, attr := range body.Attributes() {
		if attrName != "service" && attrName != "services" {
			continue
		}

		err := injectIntoExpression(body, attrName, attr, token)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}

		injected++
	}

	if injected == 0 {
		return nil, fmt.Errorf("%s: %w", name, errNoService)
	}

	return file.Bytes(), nil
}

// injectIntoExpression handles `service = { ... }`: the token is added as the last pair of the object. A list of
// objects (`services = [ { ... }, { ... } ]`) gets it in each of them.
func injectIntoExpression(body *hclwrite.Body, name string, attr *hclwrite.Attribute, token string) error {
	expr := attr.Expr().BuildTokens(nil).Bytes()

	parsed, diags := hclsyntax.ParseExpression(expr, name, hcl.InitialPos)
	if diags.HasErrors() {
		return fmt.Errorf("unable to parse the %s attribute: %w", name, diags)
	}

	var objects []*hclsyntax.ObjectConsExpr

	switch typed := parsed.(type) {
	case *hclsyntax.ObjectConsExpr:
		objects = append(objects, typed)
	case *hclsyntax.TupleConsExpr:
		for _, item := range typed.Exprs {
			object, ok := item.(*hclsyntax.ObjectConsExpr)
			if !ok {
				return fmt.Errorf("%w: %s is not a list of objects", errUnsupportedHCL, name)
			}

			objects = append(objects, object)
		}
	default:
		return fmt.Errorf("%w: %s is neither an object nor a list", errUnsupportedHCL, name)
	}

	// Insert from the last object, so that the offsets of the previous ones stay valid.
	for _, object := range slices.Backward(objects) {
		end := object.SrcRange.End.Byte - 1 // the closing brace

		if objectTokenKey.Match(expr[object.SrcRange.Start.Byte:object.SrcRange.End.Byte]) {
			return errTokenDefined
		}

		insertion := fmt.Appendf(nil, "\n  \"%s\" = %q\n", tokenAttribute, token)
		expr = bytes.Join([][]byte{expr[:end], insertion, expr[end:]}, nil)
	}

	rewritten, diags := hclwrite.ParseConfig(append([]byte(name+" = "), expr...), name, hcl.InitialPos)
	if diags.HasErrors() {
		return fmt.Errorf("unable to rewrite the %s attribute: %w", name, diags)
	}

	body.SetAttributeRaw(name, rewritten.Body().GetAttribute(name).Expr().BuildTokens(nil))

	return nil
}
