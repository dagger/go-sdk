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
	for _, name := range []string{"anchor", "module", "include"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		files[name] = string(data)
	}
	return files
}

func TestRunIncludesWritesWorkspacePaths(t *testing.T) {
	repo := t.TempDir()
	for name, contents := range map[string]string{
		"ci/go.mod":          "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"ci/main.go":         "package ci\n\nimport _ \"example.com/lib/greet\"\n",
		"lib/go.mod":         "module example.com/lib\n",
		"lib/greet/greet.go": "package greet\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte(contents), 0o644))
	}
	output := filepath.Join(t.TempDir(), "includes")
	require.NoError(t, runIncludes([]string{
		"--module-root", filepath.Join(repo, "ci"), "--workspace-path", "repo/ci", "--output", output,
	}))
	require.Equal(t, map[string]string{
		"anchor": "repo",
		"module": "ci",
		"include": `{"position":"go.mod:7","path":"../lib/go.mod","pattern":"lib/go.mod"}` + "\n" +
			`{"position":"main.go:3:8","path":"../lib/greet/*.go","pattern":"lib/greet/*.go"}` + "\n",
	}, readIncludeFiles(t, output))
}

func TestRunIncludesWithoutReplaceWritesEmptyList(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/ci\n\ngo 1.26.1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package ci\n\ntype Ci struct{}\n"), 0o644))
	output := filepath.Join(t.TempDir(), "includes")
	require.NoError(t, runIncludes([]string{
		"--module-root", root, "--workspace-path", ".", "--output", output,
	}))
	require.Equal(t, map[string]string{"anchor": ".", "module": ".", "include": ""}, readIncludeFiles(t, output))
}
