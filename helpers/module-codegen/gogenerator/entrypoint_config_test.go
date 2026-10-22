package gogenerator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteEntrypointConfig(t *testing.T) {
	schema := moduleContextSchema(t)
	root := t.TempDir()
	config := filepath.Join(root, "internal", "dagger", "entrypoint", "dang.toml")

	// A module without one gets the version it was generated for.
	require.NoError(t, writeEntrypointConfig(root, schema, "v1.0.0-beta.21"))
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	require.Contains(t, string(data), "\ndang = \"2.1\"\n\n[imports.Dagger]\ndagger = \"v1.0.0-beta.21\"\n")
	require.NotContains(t, string(data), "Code generated", "the version is the module's to change")

	// Without an explicit version, the schema's.
	require.NoError(t, os.Remove(config))
	require.NoError(t, writeEntrypointConfig(root, schema, ""))
	data, err = os.ReadFile(config)
	require.NoError(t, err)
	require.Contains(t, string(data), "dagger = \"v1.0.0\"\n")

	// An existing dang.toml is the module's declared version: kept as is.
	custom := "dang = \"2.1\"\n\n[imports.Dagger]\ndagger = \"v1.0.0-beta.20\" # pinned\n"
	require.NoError(t, os.WriteFile(config, []byte(custom), 0o644))
	require.NoError(t, writeEntrypointConfig(root, schema, "v1.0.0-beta.21"))
	data, err = os.ReadFile(config)
	require.NoError(t, err)
	require.Equal(t, custom, string(data))

	// So is one above the entrypoint, up to the module directory: the engine
	// reads the nearest.
	require.NoError(t, os.Remove(config))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dang.toml"), []byte(custom), 0o644))
	require.NoError(t, writeEntrypointConfig(root, schema, "v1.0.0-beta.21"))
	require.NoFileExists(t, config)
}
