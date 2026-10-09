package gogenerator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientPackageName(t *testing.T) {
	for _, test := range []struct {
		outputDir, packageImport, want string
	}{
		{"internal/dagger/engine-dev", "example.com/app/client", "enginedev"},
		{"clients/Cli_Dev", "example.com/app/client", "cli_dev"},
		{"", "example.com/acme/cli-dev", "clidev"},
		{".", "hello", "hello"},
		{"2fa", "example.com/app/client", "fa"},
		{"9", "example.com/app/client", "client"},
		{"go", "range", defaultClientPackageName},
		{"", "", defaultClientPackageName},
	} {
		t.Run(test.outputDir+"|"+test.packageImport, func(t *testing.T) {
			require.Equal(t, test.want, clientPackageName(test.outputDir, test.packageImport))
		})
	}
}
