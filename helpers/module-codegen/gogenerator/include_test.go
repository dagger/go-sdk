package gogenerator

import (
	"os"
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

func readTestIncludes(t *testing.T, workspacePath string, files map[string]string) (*Includes, error) {
	t.Helper()
	root := t.TempDir()
	writeModuleFiles(t, root, files)
	return ReadIncludes(root, "ci", workspacePath)
}

func TestReadIncludesGrammar(t *testing.T) {
	includes, err := readTestIncludes(t, "ci", map[string]string{
		"main.go": "//go:resources ../lib/go.mod \"../with space\" `../raw dir`\n" +
			"//go:resources\t../tabbed\n\npackage ci\n\ntype Ci struct{}\n",
	})
	require.NoError(t, err)
	var paths, patterns []string
	for _, include := range includes.Include {
		paths = append(paths, include.Path)
		patterns = append(patterns, include.Pattern)
	}
	require.Equal(t, []string{"../lib/go.mod", "../with space", "../raw dir", "../tabbed"}, paths)
	require.Equal(t, []string{"lib/go.mod", "with space", "raw dir", "tabbed"}, patterns)
	require.Equal(t, "main.go:1:1", includes.Include[0].Position)
	require.Equal(t, "main.go:2:1", includes.Include[3].Position)
}

func TestReadIncludesIgnoresOtherComments(t *testing.T) {
	includes, err := readTestIncludes(t, "ci", map[string]string{
		"main.go": "// go:resources ../a\n//go:resourcesx ../b\n/* go:resources ../c */\n/*go:resources ../d*/\n" +
			"package ci\n\ntype Ci struct{}\n\nconst text = `\n//go:resources ../e\n`\n",
	})
	require.NoError(t, err)
	require.Empty(t, includes.Include)
	require.Equal(t, "main.go", includes.MainFile)
}

func TestReadIncludesErrors(t *testing.T) {
	for name, test := range map[string]struct {
		workspacePath string
		files         map[string]string
		want          string
	}{
		"bad quote": {
			files: map[string]string{"main.go": "//go:resources \"unterminated\npackage ci\n\ntype Ci struct{}\n"},
			want:  `main.go:1:1: invalid quoted string in //go:resources: "unterminated`,
		},
		"bad escape": {
			files: map[string]string{"main.go": "//go:resources \"\\q\"\npackage ci\n\ntype Ci struct{}\n"},
			want:  `main.go:1:1: invalid quoted string in //go:resources: "\q"`,
		},
		"bad raw quote": {
			files: map[string]string{"main.go": "//go:resources `open\npackage ci\n\ntype Ci struct{}\n"},
			want:  "main.go:1:1: invalid quoted string in //go:resources: `open",
		},
		"no argument": {
			files: map[string]string{"main.go": "//go:resources\npackage ci\n\ntype Ci struct{}\n"},
			want:  "main.go:1:1: //go:resources needs at least one path",
		},
		"another file": {
			files: map[string]string{
				"main.go": "package ci\n\ntype Ci struct{}\n",
				"lib.go":  "package ci\n\n\n\n//go:resources ../lib\nfunc lib() {}\n",
			},
			want: "lib.go:5:1: //go:resources belongs in the header of main.go, the file that declares type Ci",
		},
		"after the package clause": {
			files: map[string]string{"main.go": "package ci\n\n//go:resources ../lib\ntype Ci struct{}\n"},
			want:  "main.go:3:1: //go:resources belongs in the header of main.go, the file that declares type Ci",
		},
		"a subpackage": {
			files: map[string]string{
				"main.go":    "package ci\n\ntype Ci struct{}\n",
				"sub/sub.go": "//go:resources ../lib\npackage sub\n",
			},
			want: "sub/sub.go:1:1: //go:resources belongs in the header of main.go, the file that declares type Ci",
		},
		"testdata": {
			files: map[string]string{
				"main.go":       "package ci\n\ntype Ci struct{}\n",
				"testdata/x.go": "//go:resources ../lib\npackage x\n",
			},
			want: "testdata/x.go:1:1: //go:resources belongs in the header of main.go, the file that declares type Ci",
		},
		"a dot directory": {
			files: map[string]string{
				"main.go":      "package ci\n\ntype Ci struct{}\n",
				".hidden/x.go": "//go:resources ../lib\npackage x\n",
			},
			want: ".hidden/x.go:1:1: //go:resources belongs in the header of main.go, the file that declares type Ci",
		},
		"an underscore directory": {
			files: map[string]string{
				"main.go":   "package ci\n\ntype Ci struct{}\n",
				"_old/x.go": "//go:resources ../lib\npackage x\n",
			},
			want: "_old/x.go:1:1: //go:resources belongs in the header of main.go, the file that declares type Ci",
		},
		"no main object": {
			files: map[string]string{"main.go": "//go:resources ../lib\npackage ci\n\ntype Other struct{}\n"},
			want:  "main.go:1:1: //go:resources belongs in the header of the file that declares type Ci",
		},
		"absolute": {
			files: map[string]string{"main.go": "//go:resources /x\npackage ci\n\ntype Ci struct{}\n"},
			want:  `main.go:1:1: //go:resources path "/x" is absolute; write it relative to the module directory`,
		},
		"absolute exclude": {
			files: map[string]string{"main.go": "//go:resources ../lib !/x\npackage ci\n\ntype Ci struct{}\n"},
			want:  `main.go:1:1: //go:resources path "!/x" is absolute; write it relative to the module directory`,
		},
		"leaves the workspace": {
			workspacePath: "ci",
			files:         map[string]string{"main.go": "//go:resources ../../../x\npackage ci\n\ntype Ci struct{}\n"},
			want:          `main.go:1:1: //go:resources path "../../../x" leaves the workspace root`,
		},
		"module at the workspace root": {
			workspacePath: ".",
			files:         map[string]string{"main.go": "//go:resources ../x\npackage ci\n\ntype Ci struct{}\n"},
			want:          `main.go:1:1: //go:resources path "../x" leaves the workspace root`,
		},
		"inside the module": {
			files: map[string]string{"main.go": "//go:resources data\npackage ci\n\ntype Ci struct{}\n"},
			want:  `main.go:1:1: //go:resources path "data" is inside the module directory; only "!data" is allowed there`,
		},
		"back into the module": {
			workspacePath: "apps/ci",
			files:         map[string]string{"main.go": "//go:resources ../ci/data\npackage ci\n\ntype Ci struct{}\n"},
			want:          `main.go:1:1: //go:resources path "../ci/data" is inside the module directory; only "!../ci/data" is allowed there`,
		},
		"whole module excluded": {
			files: map[string]string{"main.go": "//go:resources ../lib !.\npackage ci\n\ntype Ci struct{}\n"},
			want:  `main.go:1:1: //go:resources path "!." excludes the whole module directory`,
		},
		"empty exclude": {
			files: map[string]string{"main.go": "//go:resources ../lib !\npackage ci\n\ntype Ci struct{}\n"},
			want:  `main.go:1:1: //go:resources path "!" is empty`,
		},
		"excludes only": {
			files: map[string]string{"main.go": "//go:resources !data !../lib\npackage ci\n\ntype Ci struct{}\n"},
			want:  "main.go:1:1: //go:resources names no path outside the module directory",
		},
	} {
		t.Run(name, func(t *testing.T) {
			workspacePath := test.workspacePath
			if workspacePath == "" {
				workspacePath = "repo/ci"
			}
			_, err := readTestIncludes(t, workspacePath, test.files)
			require.EqualError(t, err, test.want)
		})
	}
}

func TestReadIncludesHeaderAndPackageDoc(t *testing.T) {
	includes, err := readTestIncludes(t, "ci", map[string]string{
		"main.go": "//go:build !ignore\n\n//go:resources ../lib/go.mod\n\n" +
			"// Package ci builds things.\n//\n//go:resources ../lib/greet\npackage ci\n\ntype Ci struct{}\n",
		"main_test.go": "package ci\n",
	})
	require.NoError(t, err)
	require.Len(t, includes.Include, 2)
	require.Equal(t, "main.go:7:1", includes.Include[1].Position)
}

func TestReadIncludesAnchor(t *testing.T) {
	for name, test := range map[string]struct {
		workspacePath string
		directive     string
		anchor        string
		modulePath    string
		include       []string
		exclude       []string
		moduleExclude []string
		resources     []string
	}{
		"one level": {
			workspacePath: "ci",
			directive:     "../lib/go.mod ../lib/greet !../lib/greet/testdata !data",
			anchor:        "..",
			modulePath:    "ci",
			include:       []string{"lib/go.mod", "lib/greet"},
			exclude:       []string{"lib/greet/testdata"},
			moduleExclude: []string{"data"},
			resources:     []string{"../lib/go.mod", "../lib/greet", "!../lib/greet/testdata", "!data"},
		},
		"two levels": {
			workspacePath: "apps/ci",
			directive:     "../lib/go.mod ../../go.mod ../../shared/*.go ../../shared/../tools !../../shared/gen",
			anchor:        "../..",
			modulePath:    "apps/ci",
			include:       []string{"apps/lib/go.mod", "go.mod", "shared/*.go", "tools"},
			exclude:       []string{"shared/gen"},
			resources:     []string{"../lib/go.mod", "../../go.mod", "../../shared/*.go", "../../tools", "!../../shared/gen"},
		},
		"exclude above the anchor": {
			workspacePath: "apps/ci",
			directive:     "../lib !../../other !../ci/data !./data",
			anchor:        "..",
			modulePath:    "ci",
			include:       []string{"lib"},
			moduleExclude: []string{"data"},
			resources:     []string{"../lib", "!data"},
		},
		"the anchor itself": {
			workspacePath: "apps/ci",
			directive:     ".. ../lib",
			anchor:        "..",
			modulePath:    "ci",
			include:       []string{"**", "lib"},
			resources:     []string{"..", "../lib"},
		},
		"duplicates": {
			workspacePath: "ci",
			directive:     "../lib ../lib/ ./../lib !../x !../x/ !data !./data",
			anchor:        "..",
			modulePath:    "ci",
			include:       []string{"lib"},
			exclude:       []string{"x"},
			moduleExclude: []string{"data"},
			resources:     []string{"../lib", "!../x", "!data"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			includes, err := readTestIncludes(t, test.workspacePath, map[string]string{
				"main.go": "//go:resources " + test.directive + "\npackage ci\n\ntype Ci struct{}\n",
			})
			require.NoError(t, err)
			var include []string
			for _, pattern := range includes.Include {
				include = append(include, pattern.Pattern)
			}
			require.Equal(t, test.anchor, includes.Anchor)
			require.Equal(t, test.modulePath, includes.ModulePath)
			require.Equal(t, test.include, include)
			require.Equal(t, test.exclude, includes.Exclude)
			require.Equal(t, test.moduleExclude, includes.ModuleExclude)
			require.Equal(t, test.resources, includes.Resources)
		})
	}
}

// Generation runs on the module directory under its anchor and has no
// workspace path; the anchor comes from the file system.
func TestReadIncludesWithoutWorkspacePath(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "apps", "ci")
	writeModuleFiles(t, root, map[string]string{
		"main.go": "//go:resources ../../shared !../ci/data\npackage ci\n\ntype Ci struct{}\n",
	})
	includes, err := ReadIncludes(root, "ci", "")
	require.NoError(t, err)
	require.Equal(t, "../..", includes.Anchor)
	require.Equal(t, "apps/ci", includes.ModulePath)
	require.Equal(t, "shared", includes.Include[0].Pattern)
	require.Equal(t, []string{"data"}, includes.ModuleExclude)
}

func TestReadIncludesWithoutDirective(t *testing.T) {
	includes, err := readTestIncludes(t, "ci", map[string]string{
		"main.go":   "package ci\n\ntype Ci struct{}\n",
		"broken.go": "package ci\n\nfunc broken( {\n",
	})
	require.NoError(t, err)
	require.Empty(t, includes.Include)
	require.Equal(t, ".", includes.Anchor)
	require.False(t, strings.Contains(includes.ModulePath, "/"))
}

func TestReadIncludesSkipsOtherModules(t *testing.T) {
	includes, err := readTestIncludes(t, "ci", map[string]string{
		"main.go":                       "//go:resources ../lib\npackage ci\n\ntype Ci struct{}\n",
		"vendor/example.com/dep/dep.go": "//go:resources ../dep\npackage dep\n",
		"tools/go.mod":                  "module example.com/tools\n",
		"tools/main.go":                 "//go:resources ../../shared\npackage main\n\ntype Ci struct{}\n",
	})
	require.NoError(t, err)
	require.Len(t, includes.Include, 1)
	require.Equal(t, "lib", includes.Include[0].Pattern)
}
