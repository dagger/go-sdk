package discovery

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeModuleFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	}
}

func coverageTree(t *testing.T, files map[string]string) string {
	t.Helper()
	buildRoot := t.TempDir()
	base := map[string]string{
		"ci/go.mod":  "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"ci/main.go": "package ci\n\nimport \"example.com/lib/greet\"\n\ntype Ci struct{}\n\nvar _ = greet.Hello\n",
	}
	for name, contents := range files {
		base[name] = contents
	}
	for name, contents := range base {
		if contents == "" {
			delete(base, name)
		}
	}
	writeModuleFiles(t, buildRoot, base)
	return filepath.Join(buildRoot, "ci")
}

func TestReadIncludes(t *testing.T) {
	lib := map[string]string{
		"lib/go.mod":         "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go": "package greet\n\nfunc Hello() {}\n",
	}
	with := func(extra map[string]string) map[string]string {
		files := map[string]string{}
		for name, contents := range lib {
			files[name] = contents
		}
		for name, contents := range extra {
			files[name] = contents
		}
		return files
	}
	covered := []string{"../lib/go.mod", "../lib/greet/*.go"}
	for name, test := range map[string]struct {
		files     map[string]string
		links     map[string]string
		resources []string
		want      string
	}{
		"imported package": {
			files: with(map[string]string{
				"lib/greet/greet_test.go": "package greet\n\nimport _ \"example.com/lib/unused\"\n",
				"lib/other/other.go":      "package other\n",
			}),
			resources: covered,
		},
		"replace go.mod missing": {
			files: map[string]string{"lib/greet/greet.go": lib["lib/greet/greet.go"]},
			want:  "go.mod:7: replace example.com/lib => ../lib reads ../lib/go.mod, which is missing",
		},
		"package missing": {
			files: map[string]string{"lib/go.mod": lib["lib/go.mod"]},
			want:  "main.go:3:8: package example.com/lib/greet is in ../lib (go.mod:7), but ../lib/greet has no Go files",
		},
		"package without Go files": {
			files: map[string]string{"lib/go.mod": lib["lib/go.mod"], "lib/greet/greeting.txt": "hello\n"},
			want:  "main.go:3:8: package example.com/lib/greet is in ../lib (go.mod:7), but ../lib/greet has no Go files",
		},
		"build-tagged import": {
			files: with(map[string]string{
				"ci/other.go":     "//go:build never\n\npackage ci\n\nimport _ \"example.com/lib/tagged\"\n",
				"lib/tagged/t.go": "package tagged\n",
			}),
			resources: append(covered, "../lib/tagged/*.go"),
		},
		"test import": {
			files: with(map[string]string{
				"ci/main_test.go":    "package ci\n\nimport _ \"example.com/lib/testutil\"\n",
				"lib/testutil/tu.go": "package testutil\n",
			}),
			resources: append(covered, "../lib/testutil/*.go"),
		},
		"transitive import": {
			files: with(map[string]string{
				"lib/greet/greet.go": "package greet\n\nimport _ \"example.com/lib/words\"\n\nfunc Hello() {}\n",
				"lib/words/words.go": "package words\n",
			}),
			resources: append(covered, "../lib/words/*.go"),
		},
		"transitive replace": {
			files: with(map[string]string{
				"ci/go.mod":          "module example.com/ci\n\ngo 1.26.1\n\nrequire (\n\texample.com/lib v0.0.0\n\texample.com/tools v0.0.0\n)\n\nreplace example.com/lib => ../lib\n\nreplace example.com/tools => ../tools\n",
				"lib/greet/greet.go": "package greet\n\nimport _ \"example.com/tools/fmt\"\n\nfunc Hello() {}\n",
				"tools/go.mod":       "module example.com/tools\n\ngo 1.26.1\n",
				"tools/fmt/fmt.go":   "package fmt\n",
			}),
			resources: append(covered, "../tools/fmt/*.go", "../tools/go.mod"),
		},
		"required by a replace target": {
			files: with(map[string]string{
				"ci/go.mod":  "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n\nreplace example.com/tools v1.0.0 => ../tools\n",
				"lib/go.mod": "module example.com/lib\n\ngo 1.26.1\n\nrequire example.com/tools v1.0.0\n",
			}),
			resources: covered,
		},
		"required but not imported": {
			files: with(map[string]string{
				"ci/go.mod":    "module example.com/ci\n\ngo 1.26.1\n\nrequire (\n\texample.com/lib v0.0.0\n\texample.com/tools v0.0.0 // indirect\n)\n\nreplace example.com/lib => ../lib\n\nreplace example.com/tools => ../tools\n",
				"tools/go.mod": "module example.com/tools\n\ngo 1.26.1\n",
			}),
			resources: append(covered, "../tools/go.mod"),
		},
		"replace not required": {
			files: with(map[string]string{
				"ci/go.mod": "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n\nreplace example.com/unused => ../unused\n",
			}),
			resources: covered,
		},
		"replace leaves the workspace": {
			files: with(map[string]string{
				"ci/go.mod": "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../../lib\n",
			}),
			want: "go.mod:7: replace example.com/lib => ../../lib leaves the workspace root",
		},
		"replace inside the module": {
			files: map[string]string{
				"ci/go.mod":                     "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ./third_party/lib\n",
				"ci/third_party/lib/go.mod":     "module example.com/lib\n",
				"ci/third_party/lib/greet/g.go": lib["lib/greet/greet.go"],
			},
		},
		"outside import from inside the module": {
			files: map[string]string{
				"ci/go.mod":                     "module example.com/ci\n\ngo 1.26.1\n\nrequire (\n\texample.com/lib v0.0.0\n\texample.com/tools v0.0.0\n)\n\nreplace example.com/lib => ./third_party/lib\n\nreplace example.com/tools => ../tools\n",
				"ci/third_party/lib/go.mod":     "module example.com/lib\n",
				"ci/third_party/lib/greet/g.go": "package greet\n\nimport _ \"example.com/tools/fmt\"\n\nfunc Hello() {}\n",
				"tools/go.mod":                  "module example.com/tools\n\ngo 1.26.1\n",
				"tools/fmt/fmt.go":              "package fmt\n",
			},
			resources: []string{"../tools/fmt/*.go", "../tools/go.mod"},
		},
		"own packages under a replaced path": {
			files: with(map[string]string{
				"ci/go.mod":        "module example.com/lib/ci\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
				"ci/sub/sub.go":    "package sub\n\nimport _ \"example.com/lib/ci/internal\"\n",
				"ci/internal/i.go": "package internal\n",
			}),
			resources: covered,
		},
		"skipped directories": {
			files: with(map[string]string{
				"ci/testdata/x/x.go": "package x\n\nimport _ \"example.com/lib/missing\"\n",
				"ci/vendor/v/v.go":   "package v\n\nimport _ \"example.com/lib/missing\"\n",
				"ci/tool/go.mod":     "module example.com/tool\n",
				"ci/tool/tool.go":    "package tool\n\nimport _ \"example.com/lib/missing\"\n",
			}),
			resources: covered,
		},
		"embeds": {
			files: with(map[string]string{
				"lib/greet/greet.go": "package greet\n\nimport \"embed\"\n\n//go:embed greeting.txt \"with space.txt\"\nvar greeting string\n\n//go:embed all:templates\nvar templates embed.FS\n\nfunc Hello() {}\n",
			}),
			resources: append(covered, "../lib/greet/greeting.txt", "../lib/greet/templates", "../lib/greet/with space.txt"),
		},
		"invalid embed": {
			files: with(map[string]string{
				"lib/greet/greet.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed \"unterminated\nvar s string\n",
			}),
			want: "../lib/greet/greet.go:5:1: invalid quoted string in //go:embed: \"unterminated",
		},
		"embed outside the package": {
			files: with(map[string]string{
				"lib/greet/greet.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed ../../../x.txt\nvar s string\n",
			}),
			want: "../lib/greet/greet.go:5:1: invalid pattern in //go:embed: ../../../x.txt",
		},
		"assembly": {
			files: with(map[string]string{
				"lib/greet/greet_amd64.s": "TEXT ·hello(SB),0,$0\n",
				"lib/greet/greet_arm64.s": "TEXT ·hello(SB),0,$0\n",
				"lib/greet/textflag.h":    "#define NOSPLIT 4\n",
			}),
			resources: append(covered, "../lib/greet/*.h", "../lib/greet/*.s"),
		},
		"replace target linked outside the workspace": {
			links: map[string]string{"lib": "$OUTSIDE/lib"},
			want:  "go.mod:7: replace example.com/lib => ../lib reads ../lib/go.mod through a symlink; symlinks outside the module directory are refused",
		},
		"replace target linked inside the workspace": {
			files: map[string]string{"real/go.mod": lib["lib/go.mod"], "real/greet/greet.go": lib["lib/greet/greet.go"]},
			links: map[string]string{"lib": "real"},
			want:  "go.mod:7: replace example.com/lib => ../lib reads ../lib/go.mod through a symlink; symlinks outside the module directory are refused",
		},
		"package linked outside the workspace": {
			files: map[string]string{"lib/go.mod": lib["lib/go.mod"]},
			links: map[string]string{"lib/greet": "$OUTSIDE/lib/greet"},
			want:  "main.go:3:8: package example.com/lib/greet reads ../lib/greet through a symlink; symlinks outside the module directory are refused",
		},
		"Go file linked outside the workspace": {
			files: map[string]string{"lib/go.mod": lib["lib/go.mod"], "lib/greet/hello.go": "package greet\n"},
			links: map[string]string{"lib/greet/link.go": "$OUTSIDE/lib/greet/greet.go"},
			want:  "../lib/greet/link.go is a symlink; symlinks outside the module directory are refused",
		},
		"embedded file linked outside the workspace": {
			files: with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar s string\n"}),
			links: map[string]string{"lib/greet/data.txt": "$OUTSIDE/lib/go.mod"},
			want:  "../lib/greet/data.go:5:1: ../lib/greet/data.txt is a symlink; symlinks outside the module directory are refused",
		},
		"embedded file linked inside the workspace": {
			files: with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar s string\n"}),
			links: map[string]string{"lib/greet/data.txt": "../go.mod"},
			want:  "../lib/greet/data.go:5:1: ../lib/greet/data.txt is a symlink; symlinks outside the module directory are refused",
		},
		"embedded directory containing a symlink": {
			files: with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed static\nvar s string\n", "lib/greet/static/a.txt": "a\n"}),
			links: map[string]string{"lib/greet/static/b.txt": "$OUTSIDE/lib/go.mod"},
			want:  "../lib/greet/data.go:5:1: ../lib/greet/static/b.txt is a symlink; symlinks outside the module directory are refused",
		},
		"symlinked embedded directory": {
			files: with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed static\nvar s string\n"}),
			links: map[string]string{"lib/greet/static": "$OUTSIDE/lib/greet"},
			want:  "../lib/greet/data.go:5:1: ../lib/greet/static is a symlink; symlinks outside the module directory are refused",
		},
		"embedded file under a symlinked directory": {
			files: with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed sub/greet.go\nvar s string\n"}),
			links: map[string]string{"lib/greet/sub": "$OUTSIDE/lib/greet"},
			want:  "../lib/greet/data.go:5:1: ../lib/greet/sub is a symlink; symlinks outside the module directory are refused",
		},
		"hidden symlink in an all: embedded directory": {
			files: with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed all:static\nvar s string\n", "lib/greet/static/a.txt": "a\n"}),
			links: map[string]string{"lib/greet/static/.hidden": "$OUTSIDE/lib/go.mod"},
			want:  "../lib/greet/data.go:5:1: ../lib/greet/static/.hidden is a symlink; symlinks outside the module directory are refused",
		},
		"hidden symlink that go:embed skips": {
			files:     with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed static\nvar s string\n", "lib/greet/static/a.txt": "a\n"}),
			links:     map[string]string{"lib/greet/static/.hidden": "$OUTSIDE/lib/go.mod"},
			resources: append(covered, "../lib/greet/static"),
		},
		"embedded directory that is a nested module": {
			files: with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed static\nvar s string\n", "lib/greet/static/go.mod": "module example.com/static\n", "lib/greet/static/a.txt": "a\n"}),
			want:  "../lib/greet/data.go:5:1: cannot embed directory ../lib/greet/static: in different module",
		},
		"nested module that go:embed skips": {
			files:     with(map[string]string{"lib/greet/data.go": "package greet\n\nimport _ \"embed\"\n\n//go:embed static\nvar s string\n", "lib/greet/static/a.txt": "a\n", "lib/greet/static/sub/go.mod": "module example.com/sub\n"}),
			links:     map[string]string{"lib/greet/static/sub/b.txt": "$OUTSIDE/lib/go.mod"},
			resources: append(covered, "../lib/greet/static"),
		},
		"no replace": {
			files: map[string]string{
				"ci/go.mod":  "module example.com/ci\n\ngo 1.26.1\n",
				"ci/main.go": "package ci\n\ntype Ci struct{}\n",
			},
		},
		"no go.mod": {
			files: map[string]string{"ci/go.mod": ""},
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := coverageTree(t, test.files)
			outside := t.TempDir()
			writeModuleFiles(t, outside, lib)
			for name, target := range test.links {
				require.NoError(t, os.Symlink(strings.ReplaceAll(target, "$OUTSIDE", outside), filepath.Join(filepath.Dir(root), name)))
			}
			includes, err := ReadIncludes(root, "ci")
			if test.want != "" {
				require.EqualError(t, err, test.want)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.resources, includes.Paths)
			if len(test.resources) == 0 {
				require.Equal(t, ".", includes.Anchor)
				require.Equal(t, ".", includes.ModulePath)
							return
			}
			require.Equal(t, "..", includes.Anchor)
			require.Equal(t, "ci", includes.ModulePath)
			for i, include := range includes.Include {
				require.Equal(t, test.resources[i], include.Path)
				require.Equal(t, path.Join("ci", include.Path), include.Pattern)
			}
		})
	}
}

// Generation runs on the module directory under its anchor and has no
// workspace path; the anchor comes from the file system.
func TestReadIncludesAnchor(t *testing.T) {
	buildRoot := t.TempDir()
	writeModuleFiles(t, buildRoot, map[string]string{
		"apps/ci/go.mod":      "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/shared v0.0.0\n\nreplace example.com/shared => ../../shared\n",
		"apps/ci/main.go":     "package ci\n\nimport _ \"example.com/shared/util\"\n",
		"shared/go.mod":       "module example.com/shared\n",
		"shared/util/util.go": "package util\n",
	})
	root := filepath.Join(buildRoot, "apps", "ci")
	for _, workspacePath := range []string{"apps/ci", ""} {
		includes, err := ReadIncludes(root, workspacePath)
		require.NoError(t, err)
		require.Equal(t, "../..", includes.Anchor)
		require.Equal(t, "apps/ci", includes.ModulePath)
		require.Equal(t, []string{"../../shared/go.mod", "../../shared/util/*.go"}, includes.Paths)
		require.Equal(t, "shared/go.mod", includes.Include[0].Pattern)
	}
}

// A symlink above the module directory leaves the paths it reads unchanged.
func TestReadIncludesThroughLinkedWorkspace(t *testing.T) {
	root := coverageTree(t, map[string]string{
		"lib/go.mod":         "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go": "package greet\n\nfunc Hello() {}\n",
	})
	link := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.Symlink(filepath.Dir(root), link))
	includes, err := ReadIncludes(filepath.Join(link, "ci"), "ci")
	require.NoError(t, err)
	require.Equal(t, []string{"../lib/go.mod", "../lib/greet/*.go"}, includes.Paths)
}

func TestReadIncludesMarksPackageGlobs(t *testing.T) {
	root := coverageTree(t, map[string]string{
		"lib/go.mod": "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go": "package greet\n\nimport (\n\t\"embed\"\n\n\t_ \"example.com/lib/greet/sub\"\n)\n\n" +
			"//go:embed assets/*.go sub/*.go\nvar files embed.FS\n\nfunc Hello() {}\n",
		"lib/greet/assets/data.go": "package assets\n",
		"lib/greet/sub/sub.go":     "package sub\n",
	})
	includes, err := ReadIncludes(root, "ci")
	require.NoError(t, err)
	packages := map[string]bool{}
	for _, include := range includes.Include {
		packages[include.Path] = include.PackageSource
	}
	require.Equal(t, map[string]bool{
		"../lib/go.mod":            false,
		"../lib/greet/*.go":        true,
		"../lib/greet/assets/*.go": false,
		"../lib/greet/sub/*.go":    true,
	}, packages)
}

func TestLocalReplaceTargets(t *testing.T) {
	root := coverageTree(t, map[string]string{
		"ci/go.mod": "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\n" +
			"replace example.com/lib => ../lib\n\nreplace example.com/unused => ../unused\n\nreplace example.com/vendored => ./third_party/vendored\n",
	})
	targets, err := LocalReplaceTargets(root)
	require.NoError(t, err)
	require.Equal(t, []string{"../lib", "../unused"}, targets)

	targets, err = LocalReplaceTargets(t.TempDir())
	require.NoError(t, err)
	require.Empty(t, targets)

	far := strings.Repeat("../", 40) + "far"
	root = coverageTree(t, map[string]string{
		"ci/go.mod": "module example.com/ci\n\ngo 1.26.1\n\nrequire example.com/far v0.0.0\n\nreplace example.com/far => " + far + "\n",
	})
	targets, err = LocalReplaceTargets(root)
	require.NoError(t, err)
	require.Equal(t, []string{far}, targets)
}
