package gogenerator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppendVCSEntries(t *testing.T) {
	require.Equal(t,
		"/dagger.gen.go linguist-generated\n/internal/dagger/** linguist-generated\n",
		string(appendVCSEntries(nil, []string{"dagger.gen.go", "internal/dagger/**"}, "/%s linguist-generated\n")),
	)

	// An entry whose text already occurs anywhere in the file is skipped, as
	// the engine skips it: a user's `-diff` variant is not duplicated, and a
	// missing trailing newline is repaired before appending.
	existing := []byte("/dagger.gen.go linguist-generated -diff")
	require.Equal(t,
		"/dagger.gen.go linguist-generated -diff\n/internal/dagger/** linguist-generated\n",
		string(appendVCSEntries(existing, []string{"dagger.gen.go", "internal/dagger/**"}, "/%s linguist-generated\n")),
	)
}

func TestAutomaticGitignore(t *testing.T) {
	for _, test := range []struct {
		name   string
		config string
		want   bool
	}{
		{name: "no config", want: true},
		{name: "default", config: "name = \"x\"\n\n[runtime]\n  source = \"go\"\n", want: true},
		{name: "table", config: "name = \"x\"\n\n[codegen]\n  automaticGitignore = false\n", want: false},
		{name: "inline table", config: "name = \"x\"\ncodegen = { automaticGitignore = false }\n", want: false},
		{name: "explicit true", config: "name = \"x\"\n\n[codegen]\nautomaticGitignore = true\n", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "mod", "src"), 0o755))
			// An ancestor's config must never decide for the module: the one
			// that counts sits at the module root, above a nested source dir.
			require.NoError(t, os.WriteFile(filepath.Join(root, moduleConfigFilename), []byte("name = \"ancestor\"\n\n[codegen]\n  automaticGitignore = false\n"), 0o600))
			if test.config != "" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "mod", moduleConfigFilename), []byte(test.config), 0o600))
			}
			got, err := automaticGitignore(root, "mod")
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
