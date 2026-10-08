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
 _ = context.Background()
}
`), 0644))
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "-buildvcs=false", "./...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
