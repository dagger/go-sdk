package gogenerator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeGoModBootstrapGrouping(t *testing.T) {
	const first = `module example.com/hello

go 1.26.1

require dagger.io/dagger v1.0.0-beta.16

require (
	github.com/99designs/gqlgen v0.17.89 // indirect
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
)

replace dagger.io/dagger => ./runtime-sdk
`
	const repeated = `module example.com/hello

go 1.26.1

require dagger.io/dagger v1.0.0-beta.16

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
)

require github.com/99designs/gqlgen v0.17.89 // indirect

replace dagger.io/dagger => ./runtime-sdk
`
	var normalized []byte
	for _, input := range []string{first, repeated} {
		path := filepath.Join(t.TempDir(), "go.mod")
		require.NoError(t, os.WriteFile(path, []byte(input), 0644))
		require.NoError(t, normalizeGoMod(path))
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		if normalized == nil {
			normalized = got
		} else {
			require.Equal(t, string(normalized), string(got))
		}
		require.NoError(t, normalizeGoMod(path))
		again, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, string(got), string(again), "normalization changed its own output")
	}
}

func TestNormalizeGoModPreservesAuthorCommentsAndDirectives(t *testing.T) {
	const input = `module example.com/hello

go 1.26.1

toolchain go1.26.2

// Direct module dependencies.
require ( // Keep the opening block note.
	// Keep this SDK pinned.
	dagger.io/dagger v1.0.0-beta.16 // SDK note
) // Keep the direct block note.

// Transitive dependencies.
require (
	// Used for configuration.
	github.com/adrg/xdg v0.5.3 // indirect
) // Keep the indirect block note.

require github.com/99designs/gqlgen v0.17.89 // indirect

// Use the checked out SDK.
replace dagger.io/dagger => ./runtime-sdk

exclude example.com/unused v1.2.3

// Broken release.
retract v1.0.1
`
	path := filepath.Join(t.TempDir(), "go.mod")
	require.NoError(t, os.WriteFile(path, []byte(input), 0644))
	require.NoError(t, normalizeGoMod(path))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, preserved := range []string{
		"toolchain go1.26.2", "Keep the opening block note.",
		"Direct module dependencies.", "Keep this SDK pinned.", "SDK note",
		"Keep the direct block note.", "Transitive dependencies.",
		"Used for configuration.", "Keep the indirect block note.",
		"Use the checked out SDK.", "replace dagger.io/dagger => ./runtime-sdk",
		"exclude example.com/unused v1.2.3", "Broken release.", "retract v1.0.1",
	} {
		require.Contains(t, string(got), preserved)
	}
	require.NoError(t, normalizeGoMod(path))
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(got), string(again), "normalization moved author comments again")
}
