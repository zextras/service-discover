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
