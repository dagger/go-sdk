package gogenerator

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
)

func TestGenerateEmbeddedClientUsesModuleBootstrap(t *testing.T) {
	gen := &GoGenerator{Config: generator.Config{
		OutputDir: t.TempDir(),
		ModuleConfig: &generator.ModuleGeneratorConfig{
			ModuleName: "hello",
		},
	}}
	state, err := gen.GenerateEmbeddedClient(
		t.Context(),
		buildClientSchema(),
		"v1.0.0-beta.11",
		"example.com/hello/internal/dagger",
	)
	require.NoError(t, err)

	generated, err := fs.ReadFile(state.Overlay, "internal/dagger/dagger.gen.go")
	require.NoError(t, err)
	source := string(generated)
	require.Equal(t, 1, strings.Count(source, "type Client struct"))
	require.Contains(t, source, "func Connect() *Client")
	require.NotContains(t, source, "func Connect(ctx context.Context")
	require.Contains(t, source, "unavailableGraphQLClient")

	_, err = fs.Stat(state.Overlay, "go.mod")
	require.ErrorIs(t, err, fs.ErrNotExist)
}
