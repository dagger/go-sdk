package gogenerator

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator/gogenerator/templates"
	"github.com/stretchr/testify/require"
)

// A client bound by path finds the Git ref of its module at run time, so its
// output names no commit and is the same from one generation to the next.
func TestUnifiedWorkspaceClientNamesNoCommit(t *testing.T) {
	generate := func() string {
		gen := &GoGenerator{Config: generator.Config{
			UnifiedClient: true, OutputDir: t.TempDir(), PackageImport: "example.com/app/client",
			ClientConfig: &generator.ClientGeneratorConfig{BoundModule: generator.BoundModule{
				Kind: generator.ModuleKindLocal, Path: ".dagger/modules/hello",
			}},
		}}
		state, err := gen.GenerateClient(t.Context(), buildClientSchema(), "v1.0.0")
		require.NoError(t, err)
		return readOverlay(t, state, "dagger.gen.go")
	}
	bindings := generate()
	require.Contains(t, bindings, `workspaceModuleClient(connection, "/.dagger/modules/hello")`)
	require.Contains(t, bindings, "func workspaceModuleRef(")
	require.NotRegexp(t, regexp.MustCompile(`[0-9a-f]{40}`), bindings)
	require.Equal(t, bindings, generate())
}

// A client of a module in another repository keeps its ref and pin, and has
// no workspace transport.
func TestUnifiedGitClientHasNoWorkspaceTransport(t *testing.T) {
	const ref, pin = "github.com/dagger/sdk-helpers@main", "64645f1967d3dba6fce951dd61ae4acd8d9b0861"
	gen := &GoGenerator{Config: generator.Config{
		UnifiedClient: true, OutputDir: t.TempDir(), PackageImport: "example.com/app/client",
		ClientConfig: &generator.ClientGeneratorConfig{BoundModule: generator.BoundModule{Kind: generator.ModuleKindGit, Ref: ref, Pin: pin}},
	}}
	state, err := gen.GenerateClient(t.Context(), buildClientSchema(), "v1.0.0")
	require.NoError(t, err)
	bindings := readOverlay(t, state, "dagger.gen.go")
	require.Contains(t, bindings, `dagger.ModuleGraphQLClient(connection, "`+ref+`", "`+pin+`")`)
	require.NotContains(t, bindings, "workspaceModuleClient")
}

// TestWorkspaceModuleRef compiles the generated workspaceModuleRef on its own
// and checks which workspaces load the module from Git.
func TestWorkspaceModuleRef(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/ref\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ref.go"), []byte("package ref\n\nimport \"strings\"\n\n"+templates.WorkspaceModuleRefHelper), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ref_test.go"), []byte(`package ref

import "testing"

func TestWorkspaceModuleRef(t *testing.T) {
	const sha = "419c4afea78603265c40e273c455623a0291922e"
	for name, tc := range map[string]struct {
		address     string
		commit      string
		holdsCaller bool
		ref, pin    string
		ok          bool
	}{
		"github workspace":                 {"github.com/o/r@" + sha, sha, true, "github.com/o/r/.dagger/modules/go@" + sha, sha, true},
		"git server workspace":             {"http://h/r.git@" + sha, sha, true, "http://h/r.git#" + sha + ":.dagger/modules/go", sha, true},
		"workspace at a branch":            {"github.com/o/r@feature/x", sha, true, "github.com/o/r/.dagger/modules/go@" + sha, sha, true},
		"workspace without a selector":     {"github.com/o/r", sha, true, "github.com/o/r/.dagger/modules/go@" + sha, sha, true},
		"scp-style workspace":              {"git@github.com:o/r@main", sha, true, "git@github.com:o/r/.dagger/modules/go@" + sha, sha, true},
		"url with userinfo, no selector":   {"https://u@h/r.git", sha, true, "https://u@h/r.git#" + sha + ":.dagger/modules/go", sha, true},
		"foreign workspace":                {"github.com/other/r@" + sha, sha, false, "", "", false},
		"local workspace":                  {"/home/u/src/r", sha, true, "", "", false},
		"relative local workspace":         {"./r", sha, true, "", "", false},
		"git workspace without a commit":   {"github.com/o/r@main", "", true, "", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			ref, pin, ok := workspaceModuleRef(tc.address, tc.commit, tc.holdsCaller, "/.dagger/modules/go")
			if ref != tc.ref || pin != tc.pin || ok != tc.ok {
				t.Fatalf("workspaceModuleRef(%q) = %q, %q, %v; want %q, %q, %v", tc.address, ref, pin, ok, tc.ref, tc.pin, tc.ok)
			}
		})
	}
}
`), 0o644))
	cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
