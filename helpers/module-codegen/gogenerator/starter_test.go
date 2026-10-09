package gogenerator

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// The default starter must compile in every entrypoint layout, with and
// without the global dag.
func TestGenerateDefaultStarter(t *testing.T) {
	schema, err := filepath.Abs("../../../cmd/dagger-go-sdk-codegen/generator/gogenerator/testdata/core/schema.json")
	require.NoError(t, err)
	starter, err := filepath.Abs("../../../templates/default")
	require.NoError(t, err)
	for _, unified := range []bool{false, true} {
		for _, global := range []bool{false, true} {
			t.Run("unified="+strconv.FormatBool(unified)+"/global="+strconv.FormatBool(global), func(t *testing.T) {
				root := t.TempDir()
				render := exec.CommandContext(t.Context(), "go", "run", ".",
					"--importable-package", "--standalone-go-module",
					"--unified-clients="+strconv.FormatBool(unified), "--global-client="+strconv.FormatBool(global),
					"hello-world", starter, root)
				render.Dir = filepath.Join("..", "..", "render-template")
				out, err := render.CombinedOutput()
				require.NoError(t, err, string(out))
				cfg := GenerateConfig{ModuleRoot: root, ModuleName: "hello-world", SchemaPath: schema, SchemaVersion: "v1.0.0-beta.16", DaggerVersion: "v1.0.0-beta.14", UnifiedClient: unified, GlobalClient: global, GoImage: "golang:1.26-alpine"}
				if unified {
					cfg.DaggerVersion = "v1.0.0-beta.16.0.20261008202843-134540fec551"
				}
				require.NoError(t, Generate(t.Context(), cfg))
			})
		}
	}
}
