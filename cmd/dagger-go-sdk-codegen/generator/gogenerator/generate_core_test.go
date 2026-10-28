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
// golden files that are the output of "cmd/codegen generate-library" in
// dagger/dagger at commit 5155ae01f8, for that schema:
//   - testdata/core: a v1.0.0-beta.14 engine.
//   - testdata/core/candidate: a dev engine built from dagger/dagger 5155ae01f8
//     with Go SDK release tooling changes that do not touch the engine.
//
// Update them the same way. Do not update them from the output of this
// generator: then the test does not check parity.
func TestGenerateCore_Parity(t *testing.T) {
	for _, dir := range []string{
		filepath.Join("testdata", "core"),
		filepath.Join("testdata", "core", "candidate"),
	} {
		t.Run(dir, func(t *testing.T) {
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
