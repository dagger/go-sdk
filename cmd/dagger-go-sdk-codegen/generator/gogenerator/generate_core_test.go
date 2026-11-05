package gogenerator

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// TestGenerateCore_Parity checks that the core bindings are the same as the
// ones that dagger/dagger generates.
//
// Each fixture directory holds an engine's introspection, schema.json, and
// the golden files that dagger/dagger's cmd/codegen at commit 5155ae01f8
// writes for that schema:
//
//	codegen generate-library --output ./sdk/go --introspection-json-path schema.json
//
// The beta.14 fixture is the introspection of a v1.0.0-beta.14 engine. The
// candidate fixture is "cmd/codegen introspect -o schema.json" run against a
// dev engine whose engine sources are those of 5155ae01f8; its SHA-256 is
// 0c1e4c41ff5e51ba8d3b8256885f4e39122137ae0f26c17700a752559ed7de8a. A new
// capture is not guaranteed to be byte-identical, so regenerate the golden
// files from the schema.json that you commit.
//
// Do not update the golden files from the output of this generator: then the
// test does not check parity.
func TestGenerateCore_Parity(t *testing.T) {
	for _, fixture := range []struct {
		name string
		dir  string
	}{
		{"beta.14", filepath.Join("testdata", "core")},
		{"candidate", filepath.Join("testdata", "core", "candidate")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			dir := fixture.dir
			data, err := os.ReadFile(filepath.Join(dir, "schema.json"))
			require.NoError(t, err)
			var resp introspection.Response
			require.NoError(t, json.Unmarshal(data, &resp))
			generator.SetSchemaParents(resp.Schema)

			gen := &GoGenerator{Config: generator.Config{
				OutputDir:     t.TempDir(),
				PackageImport: "dagger.io/dagger/core",
				CoreLibrary:   true,
			}}
			state, err := gen.GenerateCore(t.Context(), resp.Schema, resp.SchemaVersion)
			require.NoError(t, err)

			for _, file := range []string{CoreGenFile, DagGenFile} {
				got, err := fs.ReadFile(state.Overlay, file)
				require.NoError(t, err)
				want, err := os.ReadFile(filepath.Join(dir, filepath.Base(file)+".golden"))
				require.NoError(t, err)
				require.Equal(t, string(want), string(got), file)
			}
			codecs, err := fs.ReadFile(state.Overlay, CoreCodecsGenFile)
			require.NoError(t, err)
			require.Contains(t, string(codecs), "func (r *Container) UnmarshalJSON(data []byte) error")
			require.Contains(t, string(codecs), "func (r *NodeClient) UnmarshalJSON(data []byte) error")
			require.Contains(t, string(codecs), "selectNode(initRoot().QueryBuilder(), id, \"Container\")")
		})
	}
}

func TestGenerateCore_RejectsModuleTypes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "introspection", "testdata", "keep_sub1_expected_schema.json"))
	require.NoError(t, err)
	var schema introspection.Schema
	require.NoError(t, json.Unmarshal(data, &schema))
	if len(schema.DependencyNames()) == 0 {
		t.Skip("fixture has no module types")
	}
	generator.SetSchemaParents(&schema)

	gen := &GoGenerator{Config: generator.Config{CoreLibrary: true}}
	_, err = gen.GenerateCore(t.Context(), &schema, "v1.0.0")
	require.ErrorContains(t, err, "core API only")
}
