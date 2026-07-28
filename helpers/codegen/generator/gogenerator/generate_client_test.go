package gogenerator

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/introspection"
)

// newSourceMapDirective builds the @sourceMap directive the engine attaches to
// module-contributed types and fields (values are JSON-encoded).
func newSourceMapDirective(module string) *introspection.Directive {
	jsonStr := func(s string) *string {
		v := `"` + s + `"`
		return &v
	}
	return &introspection.Directive{
		Name: "sourceMap",
		Args: []*introspection.DirectiveArg{
			{Name: "module", Value: jsonStr(module)},
			{Name: "filename", Value: jsonStr("main.go")},
		},
	}
}

// buildClientSchema builds the minimal shape of a client schema: core (Query)
// plus one bound module ("hello") contributing an object type and its Query
// constructor.
func buildClientSchema() *introspection.Schema {
	schema := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{
				Kind: introspection.TypeKindObject,
				Name: "Query",
				Fields: []*introspection.Field{
					{
						Name: "hello",
						TypeRef: &introspection.TypeRef{
							Kind:   introspection.TypeKindNonNull,
							OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: "Hello"},
						},
						Directives: introspection.Directives{newSourceMapDirective("hello")},
					},
				},
			},
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Hello",
				Directives: introspection.Directives{newSourceMapDirective("hello")},
				Fields: []*introspection.Field{
					{
						Name: "hi",
						TypeRef: &introspection.TypeRef{
							Kind:   introspection.TypeKindNonNull,
							OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "String"},
						},
					},
				},
			},
		},
	}
	generator.SetSchemaParents(schema)
	return schema
}

func generateClient(t *testing.T, clientConfig *generator.ClientGeneratorConfig, outputDir string) *generator.GeneratedState {
	t.Helper()
	gen := &GoGenerator{Config: generator.Config{
		OutputDir:    outputDir,
		ClientConfig: clientConfig,
	}}
	state, err := gen.GenerateClient(t.Context(), buildClientSchema(), "v0.21.0")
	require.NoError(t, err)
	return state
}

func readOverlay(t *testing.T, state *generator.GeneratedState, path string) string {
	t.Helper()
	data, err := fs.ReadFile(state.Overlay, path)
	require.NoErrorf(t, err, "read %q from overlay", path)
	return string(data)
}

// TestGenerateClient_ServeBoundModule checks the runtime bootstrap the client
// bakes to serve the one module it is bound to (per
// hack/designs/generated-client-module-loading.md): a local module resolves
// against the workspace by a workspace-root-relative path, a git module serves
// from its canonical ref + pin, and the old dependency-serve loop /
// IncludeDependencies are gone.
func TestGenerateClient_ServeBoundModule(t *testing.T) {
	t.Run("local module resolves against the workspace by a root-relative path", func(t *testing.T) {
		state := generateClient(t, &generator.ClientGeneratorConfig{
			ModuleName:  "hello",
			BoundModule: generator.BoundModule{Kind: "DIR_SOURCE", Path: ".dagger/modules/hello"},
		}, t.TempDir())

		core := readOverlay(t, state, "dagger.gen.go")
		require.Contains(t, core, "func serveBoundModule")
		require.Contains(t, core, "CurrentWorkspace().")
		// A bare relative path is forced absolute so it resolves from the
		// workspace root (cwd-independent), not the client process's cwd.
		require.Contains(t, core, `ModuleSource("/.dagger/modules/hello").`)
		require.Contains(t, core, "AsModule().")
		// The dependency-serve loop and IncludeDependencies are gone.
		require.NotContains(t, core, "serveModuleDependencies")
		require.NotContains(t, core, "IncludeDependencies")
		require.NotContains(t, core, "ConfigExists")
	})

	t.Run("git module serves from its canonical ref + pin", func(t *testing.T) {
		state := generateClient(t, &generator.ClientGeneratorConfig{
			ModuleName:  "hello",
			BoundModule: generator.BoundModule{Kind: "GIT_SOURCE", Ref: "github.com/foo/hello@main", Pin: "abcdef"},
		}, t.TempDir())

		core := readOverlay(t, state, "dagger.gen.go")
		require.Contains(t, core, "func serveBoundModule")
		require.Contains(t, core, `ModuleSource("github.com/foo/hello@main", ModuleSourceOpts{RefPin: "abcdef"}).`)
		require.NotContains(t, core, "CurrentWorkspace")
		require.NotContains(t, core, "IncludeDependencies")
	})

	t.Run("bound module splits into its own gen file", func(t *testing.T) {
		state := generateClient(t, &generator.ClientGeneratorConfig{
			ModuleName:  "hello",
			BoundModule: generator.BoundModule{Kind: "DIR_SOURCE", Path: ".dagger/modules/hello"},
		}, t.TempDir())

		dep := readOverlay(t, state, "hello.gen.go")
		require.Contains(t, dep, "package dagger")
		require.Contains(t, dep, "type Hello struct")
		require.Contains(t, dep, "func (r *Query) Hello(")
		// The core file no longer holds the module-contributed types...
		core := readOverlay(t, state, "dagger.gen.go")
		require.NotContains(t, core, "type Hello struct")
		require.NotContains(t, core, "func (r *Query) Hello(")
		// ...but the dag convenience package (full schema) exposes the
		// module's Query constructor.
		dag := readOverlay(t, state, "dag/dag.gen.go")
		require.Contains(t, dag, "func Hello(")
	})
}

// TestGenerateClient_GoMod checks the offline go.mod handling: a fresh client
// dir gets a go.mod pinning dagger.io/dagger at the engine version; an
// existing go.mod keeps its module name and local replace directives while the
// dagger.io/dagger requirement updates (unless custom-replaced).
func TestGenerateClient_GoMod(t *testing.T) {
	clientConfig := func() *generator.ClientGeneratorConfig {
		return &generator.ClientGeneratorConfig{
			ModuleName:    "Hello",
			EngineVersion: "v0.19.2",
			BoundModule:   generator.BoundModule{Kind: "LOCAL_SOURCE", Path: "mods/hello"},
		}
	}

	t.Run("fresh dir gets go.mod with the engine pin", func(t *testing.T) {
		state := generateClient(t, clientConfig(), t.TempDir())

		goMod := readOverlay(t, state, "go.mod")
		require.Contains(t, goMod, "module hello\n")
		require.Contains(t, goMod, "go 1.")
		require.Contains(t, goMod, "require dagger.io/dagger v0.19.2")
	})

	t.Run("existing go.mod keeps module name and replaces, bumps the pin", func(t *testing.T) {
		dir := t.TempDir()
		existing := "module github.com/acme/app-client\n\ngo 1.24.0\n\nrequire dagger.io/dagger v0.18.0\n\nrequire example.com/extra v1.2.3\n\nreplace example.com/extra => ../extra\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(existing), 0o600))

		state := generateClient(t, clientConfig(), dir)

		goMod := readOverlay(t, state, "go.mod")
		require.Contains(t, goMod, "module github.com/acme/app-client\n")
		require.Contains(t, goMod, "dagger.io/dagger v0.19.2")
		require.NotContains(t, goMod, "dagger.io/dagger v0.18.0")
		require.Contains(t, goMod, "example.com/extra v1.2.3")
		require.Contains(t, goMod, "replace example.com/extra => ../extra")

		// The generated dag package imports the client by its go.mod path.
		dag := readOverlay(t, state, "dag/dag.gen.go")
		require.Contains(t, dag, `dagger "github.com/acme/app-client"`)
	})

	t.Run("custom dagger.io/dagger replace wins over the engine pin", func(t *testing.T) {
		dir := t.TempDir()
		existing := "module hello\n\ngo 1.24.0\n\nrequire dagger.io/dagger v0.18.0\n\nreplace dagger.io/dagger => ../../sdk/go\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(existing), 0o600))

		state := generateClient(t, clientConfig(), dir)

		goMod := readOverlay(t, state, "go.mod")
		require.Contains(t, goMod, "dagger.io/dagger v0.18.0")
		require.NotContains(t, goMod, "v0.19.2")
		require.Contains(t, goMod, "replace dagger.io/dagger => ../../sdk/go")
	})
}
