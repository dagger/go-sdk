package manifest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestFunctionalBuilder(t *testing.T) {
	base := New("hello")
	built := base.WithEntrypoint(DangKind, "./internal/dagger/entrypoint")

	require.Nil(t, base.Entrypoint)

	file, err := built.AsFile()
	require.NoError(t, err)
	require.Equal(t, `manifestVersion = 2
name = "hello"

[entrypoint]
  kind = "dang"
  source = "./internal/dagger/entrypoint"
`, string(file))
}

func TestManifestRequiresEntrypoint(t *testing.T) {
	_, err := New("hello").AsFile()
	require.EqualError(t, err, "entrypoint kind and source are required")
}

func TestManifestRejectsUnsupportedEntrypointKind(t *testing.T) {
	_, err := New("hello").WithEntrypoint("process", ".").AsFile()
	require.EqualError(t, err, `unsupported entrypoint kind "process"`)
}
