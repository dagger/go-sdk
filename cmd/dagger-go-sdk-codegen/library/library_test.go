package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeGoMod(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go.mod")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestReplaced(t *testing.T) {
	for name, c := range map[string]struct {
		goMod string
		want  bool
	}{
		"none":               {"module example.com/app\n\ngo 1.25\n", false},
		"unversioned":        {"module example.com/app\n\ngo 1.25\n\nreplace dagger.io/dagger => ../sdk\n", true},
		"this version":       {"module example.com/app\n\ngo 1.25\n\nreplace dagger.io/dagger v1.0.0-beta.20 => ../sdk\n", true},
		"another version":    {"module example.com/app\n\ngo 1.25\n\nreplace dagger.io/dagger v1.0.0-beta.18 => ../sdk\n", false},
		"another module":     {"module example.com/app\n\ngo 1.25\n\nreplace example.com/lib => ../lib\n", false},
		"another and this":   {"module example.com/app\n\ngo 1.25\n\nreplace (\n\tdagger.io/dagger v1.0.0-beta.18 => ../old\n\tdagger.io/dagger v1.0.0-beta.20 => ../sdk\n)\n", true},
		"versioned to proxy": {"module example.com/app\n\ngo 1.25\n\nreplace dagger.io/dagger v1.0.0-beta.20 => dagger.io/dagger v1.0.0-beta.19\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Replaced(writeGoMod(t, c.goMod), "v1.0.0-beta.20")
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestPin(t *testing.T) {
	const replace = "module example.com/app\n\ngo 1.25\n\nreplace dagger.io/dagger => ../sdk\n"
	const otherReplace = "module example.com/app\n\ngo 1.25\n\nrequire dagger.io/dagger v1.0.0-beta.20\n\nreplace dagger.io/dagger v1.0.0-beta.18 => ../sdk\n"
	const newer = "module example.com/app\n\ngo 1.25\n\nrequire dagger.io/dagger v1.0.0-beta.20\n"
	const pinned = "module example.com/app\n\ngo 1.25\n\nrequire dagger.io/dagger v1.0.0-beta.14\n"
	for name, c := range map[string]struct {
		goMod   string
		version string
		exact   bool
		want    string
	}{
		"replace":              {replace, "v1.0.0-beta.14", false, replace},
		"replace exact":        {replace, "v1.0.0-beta.14", true, replace},
		"other replace exact":  {otherReplace, "v1.0.0-beta.14", true, "module example.com/app\n\ngo 1.25\n\nrequire dagger.io/dagger v1.0.0-beta.14\n\nreplace dagger.io/dagger v1.0.0-beta.18 => ../sdk\n"},
		"newer required":       {newer, "v1.0.0-beta.14", false, newer},
		"newer required exact": {newer, "v1.0.0-beta.14", true, pinned},
		"older required exact": {"module example.com/app\n\ngo 1.25\n\nrequire dagger.io/dagger v1.0.0-beta.12\n", "v1.0.0-beta.14", true, pinned},
		"missing":              {"module example.com/app\n\ngo 1.25\n", "v1.0.0-beta.14", false, pinned},
		"unpublished":          {"module example.com/app\n\ngo 1.25\n", "v1.0.0-beta.99", false, "module example.com/app\n\ngo 1.25\n"},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeGoMod(t, c.goMod)
			require.NoError(t, Pin(path, c.version, c.exact))
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, c.want, string(got))
		})
	}
}

func TestCheckResolvable(t *testing.T) {
	dir := filepath.Dir(writeGoMod(t, "module example.com/app\n\ngo 1.26.1\n"))
	require.NoError(t, CheckResolvable(dir, `module "app"`, "v1.0.0-beta.17"))
	err := CheckResolvable(dir, `module "app"`, "v1.0.0-beta.99")
	require.ErrorContains(t, err, `module "app" targets Dagger v1.0.0-beta.99, and its Go library dagger.io/dagger v1.0.0-beta.99 cannot be resolved (`)
	require.ErrorContains(t, err, `unknown revision v1.0.0-beta.99): set targetVersion to a release whose library is published in the scope's settings in dagger.toml, or replace dagger.io/dagger in go.mod for development`)
}

// A requirement of another module raises the selected library above the
// target; a replace directive for a version the build does not select grants
// no exception.
func TestCheckSelection(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"app/go.mod":         "module example.com/app\n\ngo 1.26.1\n\nrequire (\n\tdagger.io/dagger v1.0.0-beta.17\n\texample.com/lib v0.0.0\n)\n\nreplace example.com/lib => ../lib\n\nreplace dagger.io/dagger v1.0.0-beta.16 => dagger.io/dagger v1.0.0-beta.15\n",
		"app/main.go":        "package app\n\nimport (\n\t_ \"dagger.io/dagger\"\n\t_ \"example.com/lib/greet\"\n)\n",
		"lib/go.mod":         "module example.com/lib\n\ngo 1.26.1\n\nrequire dagger.io/dagger v1.0.0-beta.18\n",
		"lib/greet/greet.go": "package greet\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644))
	}
	app := filepath.Join(root, "app")
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = app
	out, err := tidy.CombinedOutput()
	require.NoError(t, err, string(out))
	replaced, err := Replaced(filepath.Join(app, "go.mod"), "v1.0.0-beta.17")
	require.NoError(t, err)
	require.False(t, replaced)
	require.EqualError(t, CheckSelection(t.Context(), app, `scope "ci"`, "v1.0.0-beta.17"),
		`scope "ci" targets Dagger v1.0.0-beta.17, and example.com/lib v0.0.0 requires dagger.io/dagger v1.0.0-beta.18: set targetVersion to v1.0.0-beta.18 or later in the scope's settings in dagger.toml, or require a version of example.com/lib that needs dagger.io/dagger v1.0.0-beta.17 or earlier`)
	require.NoError(t, CheckSelection(t.Context(), app, `scope "ci"`, "v1.0.0-beta.18"))
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(name)), contents)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

// Dependency clients alias core types from the running engine's schema. One
// the targeted release lacks is refused with the dependency and the target
// named, instead of a compile error in generated code.
func TestCheckCoreNames(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.26.1\n\nrequire dagger.io/dagger v1.0.0-beta.17\n",
		"main.go": "package app\n\nimport _ \"dagger.io/dagger/core\"\n",
		"internal/dagger/dagger.gen.go": "// Code generated by dagger. DO NOT EDIT.\n\npackage dagger\n\nimport \"dagger.io/dagger/core\"\n\n" +
			"type Container = core.Container\ntype ContainerID = core.ID\ntype DaggerObject = core.DaggerObject\n\nconst Shared = core.CacheSharingModeShared\n",
		"internal/dagger/clients/client-dep/dagger.gen.go": "// Code generated by dagger. DO NOT EDIT.\n\npackage clientdep\n\nimport (\n\t\"dagger.io/dagger/core\"\n\tdep0 \"example.com/app/internal/other\"\n)\n\n" +
			"type Directory = core.Directory\ntype OtherClient = dep0.Client\n",
		"internal/other/other.go": "package other\n\ntype Client struct{}\n",
	})
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = root
	out, err := tidy.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, CheckCoreNames(t.Context(), root, `module "app"`, "v1.0.0-beta.17"))

	writeFile(t, filepath.Join(root, "internal", "dagger", "clients", "client-dep", "dagger.gen.go"),
		"// Code generated by dagger. DO NOT EDIT.\n\npackage clientdep\n\nimport \"dagger.io/dagger/core\"\n\ntype Directory = core.Directory\ntype Future = core.Future\ntype Later = core.Later\ntype LaterID = core.ID\n")
	require.EqualError(t, CheckCoreNames(t.Context(), root, `module "app"`, "v1.0.0-beta.17"),
		`module "app" targets Dagger v1.0.0-beta.17, and dependency "client-dep" exposes Future and Later, which that release does not have: set targetVersion to a release that has them, such as the running engine's, in the scope's settings in dagger.toml, or use a version of client-dep that does not expose them`)

	writeFile(t, filepath.Join(root, "internal", "dagger", "dagger.gen.go"),
		"// Code generated by dagger. DO NOT EDIT.\n\npackage dagger\n\nimport \"dagger.io/dagger/core\"\n\ntype Container = core.Container\n\nconst Soon = core.Soon\n")
	require.EqualError(t, CheckCoreNames(t.Context(), root, `scope "ci"`, "v1.0.0-beta.17"),
		`scope "ci" targets Dagger v1.0.0-beta.17, and its code uses Soon, which that release does not have: set targetVersion to a release that has it, such as the running engine's, in the scope's settings in dagger.toml, or stop using it`)
}
