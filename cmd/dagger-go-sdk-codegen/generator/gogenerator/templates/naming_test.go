package templates

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// The engine is asked for the arguments the bindings pass, named as the
// parser names them: an unnamed param by its type, and a pointer to an
// inline struct by its fields.
func TestModuleDeclarationsNameArgumentsAsParsed(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", `package main

import "context"

type Main struct{}

type Finder interface {
	Count(context.Context, string) (int, error)
	Find(ctx context.Context, opts *struct{ Path string }) (string, error)
}
`, 0)
	require.NoError(t, err)
	pkg, err := (&types.Config{Importer: importer.ForCompiler(fset, "source", nil)}).Check("example.com/m", fset, []*ast.File{file}, nil)
	require.NoError(t, err)
	typeNames, fieldNames, candidates := ModuleDeclarations(&packages.Package{Types: pkg}, fset)
	require.Equal(t, []string{"Finder", "Main"}, typeNames)
	require.Equal(t, []string{"ID", "Count", "string", "Find", "Path"}, fieldNames)
	require.Equal(t, []string{"Main"}, candidates)
}

// A nullable object is loaded by its schema name, which its Go name can
// spell differently: a dependency named by strcase keeps DepGpuThing, which
// the engine's rules style as DepGPUThing in Go.
func TestNullableObjectLoadsBySchemaName(t *testing.T) {
	var foo, thing introspection.Type
	require.NoError(t, json.Unmarshal([]byte(`{
  "kind": "OBJECT",
  "name": "Foo",
  "fields": [{"name": "maybe", "args": [], "type": {"kind": "OBJECT", "name": "DepGpuThing"}}]
}`), &foo))
	require.NoError(t, json.Unmarshal([]byte(`{
  "kind": "OBJECT",
  "name": "DepGpuThing",
  "fields": [{"name": "id", "args": [], "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}}]
}`), &thing))
	schema := &introspection.Schema{Types: introspection.Types{&foo, &thing}}
	generator.SetSchemaParents(schema)
	generator.SetSchema(schema)
	t.Cleanup(func() { generator.SetSchema(nil) })
	funcs := GoTemplateFuncs(schema, nil, "v1.0.0-beta.22", generator.Config{Names: styled})
	tmpl, err := template.New("object.go.tmpl").Funcs(funcs).ParseFiles(filepath.Join("src", "_types/object.go.tmpl"))
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, &foo))
	require.NoError(t, styled.Err())
	got := buf.String()
	require.Contains(t, got, "func (r *Foo) Maybe(ctx context.Context) (*DepGPUThing, error)")
	require.Contains(t, got, `selectNode(q.Root(), *objectID, "DepGpuThing")`)
}
