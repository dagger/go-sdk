package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func readIncludeFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"anchor", "module", "include", "exclude", "module-exclude"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		files[name] = string(data)
	}
	return files
}

func TestRunIncludesWritesWorkspacePaths(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(
		"//go:mod:include ../lib/go.mod \"../lib/with space\" !../lib/greet/testdata !data\npackage ci\n\ntype Ci struct{}\n",
	), 0o644))
	output := filepath.Join(t.TempDir(), "includes")
	require.NoError(t, runIncludes([]string{
		"--module-root", root, "--module-name", "ci", "--workspace-path", "repo/ci", "--output", output,
	}))
	require.Equal(t, map[string]string{
		"anchor": "repo",
		"module": "ci",
		"include": `{"position":"main.go:1:1","path":"../lib/go.mod","pattern":"lib/go.mod"}` + "\n" +
			`{"position":"main.go:1:1","path":"../lib/with space","pattern":"lib/with space"}` + "\n",
		"exclude":        `"lib/greet/testdata"` + "\n",
		"module-exclude": `"data"` + "\n",
	}, readIncludeFiles(t, output))
}

func TestRunIncludesWithoutDirectiveWritesEmptyLists(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package ci\n\ntype Ci struct{}\n"), 0o644))
	output := filepath.Join(t.TempDir(), "includes")
	require.NoError(t, runIncludes([]string{
		"--module-root", root, "--module-name", "ci", "--workspace-path", ".", "--output", output,
	}))
	require.Equal(t, map[string]string{
		"anchor": ".", "module": ".", "include": "", "exclude": "", "module-exclude": "",
	}, readIncludeFiles(t, output))
}
