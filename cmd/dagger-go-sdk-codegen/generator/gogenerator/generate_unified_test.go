package gogenerator

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
	"github.com/stretchr/testify/require"
)

func TestUnifiedClientSharesCoreObjects(t *testing.T) {
	for _, schemaVersion := range []string{"v0.18.0", "v0.21.10", "v1.0.0-beta.15"} {
		t.Run(schemaVersion, func(t *testing.T) {
			testUnifiedClientSharesCoreObjects(t, schemaVersion)
		})
	}
}

func TestUnifiedClientPinsAreIndependentOfTargetVersion(t *testing.T) {
	const ref = "github.com/dagger/sdk-helpers@main"
	for _, pin := range []string{"64645f1967d3dba6fce951dd61ae4acd8d9b0861", "92d07fcb9dd25ae50afd30097f09936d674c2950"} {
		gen := &GoGenerator{Config: generator.Config{
			UnifiedClient: true, OutputDir: t.TempDir(), PackageImport: "example.com/app/client",
			ClientConfig: &generator.ClientGeneratorConfig{BoundModule: generator.BoundModule{
				Kind: generator.ModuleKindGit, Ref: ref, Pin: pin,
			}},
		}}
		state, err := gen.GenerateClient(t.Context(), buildClientSchema(), "v0.18.0")
		require.NoError(t, err)
		bindings := readOverlay(t, state, "dagger.gen.go")
		require.Contains(t, bindings, `dagger.ModuleGraphQLClient(connection, "`+ref+`", "`+pin+`")`)
		require.NotContains(t, bindings, ".AsModule().Serve(")
	}
}

func TestUnifiedClientConstructorShortcut(t *testing.T) {
	for _, schemaVersion := range []string{"v0.18.0", "v0.21.10", "v1.0.0-beta.15"} {
		t.Run(schemaVersion, func(t *testing.T) {
			bindings := generateUnifiedHello(t, buildClientSchema(), schemaVersion, true)
			require.Contains(t, bindings, "func NewHello() *Hello {\n\treturn New().Hello()\n}")
		})
	}
}

func TestUnifiedClientConstructorShortcutSignatures(t *testing.T) {
	schema := buildClientSchema()
	nonNull := func(kind introspection.TypeKind, name string) *introspection.TypeRef {
		return &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: kind, Name: name}}
	}
	expected, defaultName := `"Container"`, `"world"`
	schema.Types = append(schema.Types, &introspection.Type{Kind: introspection.TypeKindObject, Name: "Container"})
	query := schema.Query()
	query.Fields[0].Args = introspection.InputValues{
		{Name: "ctr", TypeRef: nonNull(introspection.TypeKindScalar, "ID"), Directives: []*introspection.Directive{{Name: "expectedType", Args: []*introspection.DirectiveArg{{Name: "name", Value: &expected}}}}},
		{Name: "name", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "String"}, DefaultValue: &defaultName},
	}
	query.Fields = append(query.Fields,
		&introspection.Field{Name: "helloVersion", TypeRef: nonNull(introspection.TypeKindScalar, "String"), Directives: introspection.Directives{newSourceMapDirective("hello")}},
		&introspection.Field{Name: "container", TypeRef: nonNull(introspection.TypeKindObject, "Hello"), Directives: introspection.Directives{newSourceMapDirective("hello")}},
	)
	generator.SetSchemaParents(schema)
	bindings := generateUnifiedHello(t, schema, "v1.0.0-beta.15", true)
	require.Contains(t, bindings, "func NewHello(ctr *Container, opts ...HelloOpts) *Hello {\n\treturn New().Hello(ctr, opts...)\n}")
	require.Contains(t, bindings, "func HelloVersion(ctx context.Context) (string, error) {\n\treturn New().HelloVersion(ctx)\n}")
	// A field named like a core alias must not redeclare it.
	require.Contains(t, bindings, "func NewContainer() *Hello {\n\treturn New().Container()\n}")
}

func TestUnifiedClientConstructorShortcutSkipsCollisions(t *testing.T) {
	schema := buildClientSchema()
	schema.Types = append(schema.Types, &introspection.Type{Kind: introspection.TypeKindObject, Name: "NewHello", Directives: introspection.Directives{newSourceMapDirective("hello")}})
	generator.SetSchemaParents(schema)
	bindings := generateUnifiedHello(t, schema, "v1.0.0-beta.15", true)
	require.Contains(t, bindings, "type NewHello struct")
	require.Contains(t, bindings, "func (r *Query) Hello() *Hello")
	require.NotContains(t, bindings, "func NewHello(")
}

// Adding a client must never change the module's own package, so only
// packages bound to a module get constructors.
func TestUnifiedModulePackageHasNoConstructorShortcut(t *testing.T) {
	require.Contains(t, generateUnifiedHello(t, buildClientSchema(), "v1.0.0-beta.15", true), "func NewHello() *Hello")
	gen := &GoGenerator{Config: generator.Config{UnifiedClient: true, OutputDir: t.TempDir(), ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "hello"}}}
	state, err := gen.GenerateEmbeddedClient(t.Context(), buildClientSchema(), "v1.0.0-beta.15", "example.com/hello/internal/dagger")
	require.NoError(t, err)
	own := readOverlay(t, state, "internal/dagger/hello.gen.go")
	require.Contains(t, own, "func (r *Query) Hello() *Hello")
	require.NotContains(t, own, "func NewHello(")
}

func TestEmbeddedClientHasNoConstructorShortcut(t *testing.T) {
	bindings := generateUnifiedHello(t, buildClientSchema(), "v1.0.0-beta.15", false)
	require.Contains(t, bindings, "func (r *Query) Hello() *Hello")
	require.NotContains(t, bindings, "func NewHello(")
}

func generateUnifiedHello(t *testing.T, schema *introspection.Schema, schemaVersion string, unified bool) string {
	t.Helper()
	gen := &GoGenerator{Config: generator.Config{UnifiedClient: unified, OutputDir: t.TempDir(), PackageImport: "example.com/app/client", ClientConfig: &generator.ClientGeneratorConfig{BoundModule: generator.BoundModule{Kind: generator.ModuleKindDir, Path: "hello"}}}}
	state, err := gen.GenerateClient(t.Context(), schema, schemaVersion)
	require.NoError(t, err)
	return readOverlay(t, state, "hello.gen.go")
}

func testUnifiedClientSharesCoreObjects(t *testing.T, schemaVersion string) {
	root := t.TempDir()
	schema := buildClientSchema()
	ref := func(name string) *introspection.TypeRef {
		return &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: name}}
	}
	schema.Types = append(schema.Types, &introspection.Type{Kind: introspection.TypeKindObject, Name: "Container"})
	expected := `"Container"`
	schema.Types[1].Fields = append(schema.Types[1].Fields, &introspection.Field{Name: "echo", TypeRef: ref("Container"), Args: introspection.InputValues{{Name: "value", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ID"}}, Directives: []*introspection.Directive{{Name: "expectedType", Args: []*introspection.DirectiveArg{{Name: "name", Value: &expected}}}}}}})
	gen := &GoGenerator{Config: generator.Config{UnifiedClient: true, OutputDir: root, PackageImport: "example.com/app/client", ClientConfig: &generator.ClientGeneratorConfig{BoundModule: generator.BoundModule{Kind: generator.ModuleKindDir, Path: "hello"}}}}
	state, err := gen.GenerateClient(t.Context(), schema, schemaVersion)
	require.NoError(t, err)
	require.NoError(t, generator.Overlay(t.Context(), state.Overlay, filepath.Join(root, "client")))
	bindings := readOverlay(t, state, "hello.gen.go")
	require.Contains(t, bindings, "func (r *Hello) Echo(value *Container) *Container")
	require.Contains(t, bindings, "WithGraphQLQuery(q)")
	require.NotContains(t, bindings, "type Container struct")
	coreBindings := readOverlay(t, state, "dagger.gen.go")
	require.Contains(t, coreBindings, `dagger.ModuleGraphQLClient(connection, "/hello", "")`)
	require.NotContains(t, coreBindings, "func (r *Query) Container(")
	_, err = fs.Stat(state.Overlay, "dag/dag.gen.go")
	require.ErrorIs(t, err, fs.ErrNotExist)
	sdkPath := os.Getenv("GO_SDK_TEST_RUNTIME")
	if sdkPath == "" {
		t.Skip("set GO_SDK_TEST_RUNTIME to a checkout of the client split to compile the generated package")
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n\nrequire dagger.io/dagger v1.0.0-beta.15\nreplace dagger.io/dagger => "+sdkPath+"\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "client_test.go"), []byte(`package app_test
import("context";"testing";"dagger.io/dagger/core";"example.com/app/client")
func TestObjectIdentity(t *testing.T){
 var got *core.Container = client.New().Hello().Echo(core.NewContainer())
 _ = got
 var shortcut *core.Container = client.NewHello().Echo(core.NewContainer())
 _ = shortcut
 _ = context.Background()
}
`), 0644))
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "-buildvcs=false", "./...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// TestUnifiedClientRemembersIDs checks that a unified client generated from a
// schema that declares @reevaluate defines what its bindings use to remember
// IDs: the memo helper, and the flag on Query.
func TestUnifiedClientRemembersIDs(t *testing.T) {
	root := t.TempDir()
	schema := buildClientSchema()
	schema.Directives = append(schema.Directives, &introspection.DirectiveDef{Name: "reevaluate"})
	hello := schema.Types[1]
	hello.Fields = append(hello.Fields,
		&introspection.Field{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ID"}}},
		&introspection.Field{
			Name:       "fresh",
			TypeRef:    &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: "Hello"}},
			Directives: introspection.Directives{{Name: "reevaluate"}},
		},
	)
	generator.SetSchemaParents(schema)
	gen := &GoGenerator{Config: generator.Config{UnifiedClient: true, OutputDir: root, PackageImport: "example.com/app/client", ClientConfig: &generator.ClientGeneratorConfig{BoundModule: generator.BoundModule{Kind: generator.ModuleKindDir, Path: "hello"}}}}
	state, err := gen.GenerateClient(t.Context(), schema, "v1.0.0")
	require.NoError(t, err)
	require.NoError(t, generator.Overlay(t.Context(), state.Overlay, filepath.Join(root, "client")))

	bindings := readOverlay(t, state, "hello.gen.go")
	require.Contains(t, bindings, "memoizedID(ctx, r.query,")
	require.Contains(t, bindings, "refetchID: r.refetchID,")
	require.Contains(t, bindings, "refetchID: true,")
	coreBindings := readOverlay(t, state, "dagger.gen.go")
	require.Contains(t, coreBindings, "func memoizedID[")
	require.Contains(t, coreBindings, "refetchID bool")

	sdkPath := os.Getenv("GO_SDK_TEST_RUNTIME")
	if sdkPath == "" {
		t.Skip("set GO_SDK_TEST_RUNTIME to a checkout of the client split to compile the generated package")
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n\nrequire dagger.io/dagger v1.0.0-beta.15\nreplace dagger.io/dagger => "+sdkPath+"\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "client_test.go"), []byte(`package app_test
import("testing";"example.com/app/client")
func TestCompiles(t *testing.T){ _ = client.New().Hello().Fresh() }
`), 0644))
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "-buildvcs=false", "./...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
