package gogenerator

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// TestGenerateCore_Parity checks that the core bindings are the same as the
// ones that dagger/dagger generates.
//
// testdata/core/schema.json is the introspection of a v1.0.0-beta.14 engine.
// The golden files are the output of "cmd/codegen generate-library" in
// dagger/dagger at commit 0bf4122718 (dagger/dagger#14593), for that schema.
// Update them the same way. Do not update them from the output of this
// generator: then the test does not check parity.
//
// The engine names Go identifiers; the fake one here spells each name as the
// golden files do, so the test checks the bindings around them.
func TestGenerateCore_Parity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "core", "schema.json"))
	require.NoError(t, err)
	var resp introspection.Response
	require.NoError(t, json.Unmarshal(data, &resp))
	generator.SetSchemaParents(resp.Schema)

	golden := map[string]string{}
	// The golden files' spellings of identifiers, by spelling lowercased:
	// the most frequent in code, outside comments and strings.
	counts := map[string]map[string]int{}
	code := regexp.MustCompile(`"(?:[^"\\]|\\.)*"|//.*`)
	for _, file := range []string{CoreGenFile, DagGenFile} {
		want, err := os.ReadFile(filepath.Join("testdata", "core", filepath.Base(file)+".golden"))
		require.NoError(t, err)
		golden[file] = string(want)
		for _, ident := range regexp.MustCompile(`\b[A-Z][A-Za-z0-9]*\b`).FindAllString(code.ReplaceAllString(string(want), ""), -1) {
			key := strings.ToLower(ident)
			if counts[key] == nil {
				counts[key] = map[string]int{}
			}
			counts[key][ident]++
		}
	}
	format := func(name string) string {
		spelled, most := identifierstest.Format(name), 0
		for ident, n := range counts[strings.ToLower(strings.ReplaceAll(name, "_", ""))] {
			if n > most || n == most && ident < spelled {
				spelled, most = ident, n
			}
		}
		return spelled
	}

	gen := &GoGenerator{Config: generator.Config{
		OutputDir:     t.TempDir(),
		PackageImport: "dagger.io/dagger/core",
		CoreLibrary:   true,
		Names:         identifiers.NewNames(t.Context(), identifierstest.Formatting(format), resp.SchemaVersion),
	}}
	state, err := gen.GenerateCore(t.Context(), resp.Schema, resp.SchemaVersion)
	require.NoError(t, err)

	for _, file := range []string{CoreGenFile, DagGenFile} {
		t.Run(file, func(t *testing.T) {
			got, err := fs.ReadFile(state.Overlay, file)
			require.NoError(t, err)
			require.Equal(t, golden[file], string(got))
		})
	}
	codecs, err := fs.ReadFile(state.Overlay, CoreCodecsGenFile)
	require.NoError(t, err)
	require.Contains(t, string(codecs), "func (r *Container) UnmarshalJSON(data []byte) error")
	require.Contains(t, string(codecs), "func (r *NodeClient) UnmarshalJSON(data []byte) error")
	require.Contains(t, string(codecs), "selectNode(initRoot().QueryBuilder(), id, \"Container\")")
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

	gen := &GoGenerator{Config: generator.Config{Names: styled, CoreLibrary: true}}
	_, err = gen.GenerateCore(t.Context(), &schema, "v1.0.0")
	require.ErrorContains(t, err, "core API only")
}
