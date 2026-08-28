package gogenerator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoLanguageVersion(t *testing.T) {
	for _, test := range []struct {
		toolchain string
		want      string
		wantErr   bool
	}{
		{toolchain: "go1.26.7", want: "1.26"},
		{toolchain: "go1.26rc1", want: "1.26"},
		{toolchain: "go1.26", want: "1.26"},
		{toolchain: "devel go1.27-abcdef 2026-01-01", want: "1.27"},
		{toolchain: "not a go version", wantErr: true},
	} {
		t.Run(test.toolchain, func(t *testing.T) {
			got, err := goLanguageVersion(test.toolchain)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
