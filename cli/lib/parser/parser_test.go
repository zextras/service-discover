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

package parser

import (
	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

func TestErrorWithRandomFormat(t *testing.T) {
	t.Parallel()

	// 0 is for plain output
	// 1 is for json output
	cases := []struct {
		name                string
		formatValue         string
		expectedParsedValue int
		expectsError        bool
	}{
		{"PlainRender format", "plain", 0, false},
		{"JsonRender format", "json", 1, false},
		// The expectedParsedValue here is random, it doesn't have sense!
		{"Non valid format", "invalidStuff", 1, true},
	}

	format := format{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			decoder := &kong.DecodeContext{
				Value: nil,
				Scan:  kong.Scan(c.formatValue),
			}

			// We need this to be able to perform reflection on an *int type with non-zero value
			formatResult := -1
			if c.expectsError {
				res := format.Decode(decoder, reflect.ValueOf(&formatResult).Elem())
				assert.NotNil(t, res)
				// formatResult must not change after this operation
				assert.Equal(t, -1, formatResult)
			} else {
				assert.Nil(t, format.Decode(decoder, reflect.ValueOf(&formatResult).Elem()))
				assert.EqualValues(t, c.expectedParsedValue, formatResult)
			}
		})
	}
}
