// SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package spire_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zextras/service-discover/pkg/spire"
)

func TestParseJWTSVID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{name: "the token is on the line after the header", output: prettyJWTOutput, want: "eyJhbGciOi.payload.sig"},
		{name: "a missing header is an error", output: "eyJhbGciOi.payload.sig\n", wantErr: true},
		{name: "a missing token is an error", output: "token(spiffe://carbonio/consul/agent-h):", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := spire.ParseJWTSVID(tt.output)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
