package gogenerator

import (
	"encoding/json"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
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
	return generateClientForVersion(t, clientConfig, outputDir, "v1.0.0")
}

func generateClientForVersion(t *testing.T, clientConfig *generator.ClientGeneratorConfig, outputDir, schemaVersion string) *generator.GeneratedState {
	t.Helper()
	gen := &GoGenerator{Config: generator.Config{
		OutputDir:     outputDir,
		PackageImport: "example.com/client",
		ClientConfig:  clientConfig,
	}}
	state, err := gen.GenerateClient(t.Context(), buildClientSchema(), schemaVersion)
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

	t.Run("legacy local module resolves through Query from the client cwd", func(t *testing.T) {
		state := generateClientForVersion(t, &generator.ClientGeneratorConfig{
			BoundModule: generator.BoundModule{Kind: "DIR_SOURCE", Path: ".dagger/modules/hello"},
		}, t.TempDir(), "v0.17.1")

		core := readOverlay(t, state, "dagger.gen.go")
		require.NotContains(t, core, "CurrentWorkspace().")
		require.Contains(t, core, `ModuleSource(".dagger/modules/hello").`)
	})

	t.Run("git module serves from its canonical ref + pin", func(t *testing.T) {
		state := generateClient(t, &generator.ClientGeneratorConfig{
			BoundModule: generator.BoundModule{Kind: "GIT_SOURCE", Ref: "github.com/foo/hello@main", Pin: "abcdef"},
		}, t.TempDir())

		core := readOverlay(t, state, "dagger.gen.go")
		require.Contains(t, core, "func serveBoundModule")
		require.Contains(t, core, `ModuleSource("github.com/foo/hello@main", ModuleSourceOpts{RefPin: "abcdef"}).`)
		require.NotContains(t, core, "CurrentWorkspace")
		require.NotContains(t, core, "IncludeDependencies")
	})

	t.Run("legacy Void serve result is discarded", func(t *testing.T) {
		state := generateClientForVersion(t, &generator.ClientGeneratorConfig{
			BoundModule: generator.BoundModule{Kind: "GIT_SOURCE", Ref: "github.com/foo/hello@main", Pin: "abcdef"},
		}, t.TempDir(), "v0.9.11")

		core := readOverlay(t, state, "dagger.gen.go")
		require.Contains(t, core, "_, err := client.")
		require.Contains(t, core, "Serve(ctx)\n\treturn err")
	})

	t.Run("bound module splits into its own gen file", func(t *testing.T) {
		state := generateClient(t, &generator.ClientGeneratorConfig{
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

func TestGenerateClient_PackageMode(t *testing.T) {
	outputDir := t.TempDir()
	gen := &GoGenerator{Config: generator.Config{
		OutputDir:     outputDir,
		PackageImport: "example.com/app/internal/dagger/clients/hello",
		ClientConfig: &generator.ClientGeneratorConfig{
			BoundModule: generator.BoundModule{Kind: "GIT_SOURCE", Ref: "github.com/foo/hello", Pin: "abcdef"},
		},
	}}
	state, err := gen.GenerateClient(t.Context(), buildClientSchema(), "v0.21.0")
	require.NoError(t, err)

	_, err = fs.Stat(state.Overlay, "go.mod")
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.Contains(
		t,
		readOverlay(t, state, "dag/dag.gen.go"),
		`dagger "example.com/app/internal/dagger/clients/hello"`,
	)
}

// TestGenerateClient_DoesNotCallDaggerClientQueryBuilder type-checks a client
// generated from the core schema, so it catches a call to
// dagger.Client.QueryBuilder() through any expression. dagger/dagger#14186
// removes that method.
func TestGenerateClient_DoesNotCallDaggerClientQueryBuilder(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "core", "schema.json"))
	require.NoError(t, err)
	var resp introspection.Response
	require.NoError(t, json.Unmarshal(data, &resp))
	generator.SetSchemaParents(resp.Schema)

	// The generated files go into an overlay inside this module, so that
	// go/packages resolves dagger.io/dagger from this module's go.mod.
	wd, err := os.Getwd()
	require.NoError(t, err)
	gen := &GoGenerator{Config: generator.Config{
		OutputDir:     t.TempDir(),
		PackageImport: "github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator/gogenerator/generatedclient",
		ClientConfig:  &generator.ClientGeneratorConfig{},
	}}
	state, err := gen.GenerateClient(t.Context(), resp.Schema, resp.SchemaVersion)
	require.NoError(t, err)

	overlay := map[string][]byte{}
	require.NoError(t, fs.WalkDir(state.Overlay, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		overlay[filepath.Join(wd, "generatedclient", path)] = []byte(readOverlay(t, state, path))
		return nil
	}))

	pkgs, err := packages.Load(&packages.Config{
		Mode:    packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo,
		Overlay: overlay,
	}, "./generatedclient/...")
	require.NoError(t, err)
	require.Len(t, pkgs, 2)
	for _, pkg := range pkgs {
		require.Emptyf(t, pkg.Errors, "%s does not type-check", pkg.PkgPath)
		for expr, sel := range pkg.TypesInfo.Selections {
			fn, ok := sel.Obj().(*types.Func)
			if ok && fn.FullName() == "(*dagger.io/dagger.Client).QueryBuilder" {
				t.Errorf("%s calls dagger.Client.QueryBuilder()", pkg.Fset.Position(expr.Pos()))
			}
		}
	}
}
