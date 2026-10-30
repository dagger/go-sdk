package templates

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

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
