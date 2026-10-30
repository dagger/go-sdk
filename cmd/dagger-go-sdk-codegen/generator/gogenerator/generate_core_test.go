package gogenerator

import (
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// TestGenerateCore_Parity checks that the core bindings are the same as the
// ones that dagger/dagger generates, but for the Go identifiers the engine
// spells differently.
//
// testdata/core/schema.json is the introspection of a v1.0.0-beta.14 engine.
// The golden files are the output of "cmd/codegen generate-library" in
// dagger/dagger at commit 0bf4122718 (dagger/dagger#14593), for that schema.
// Update them the same way. Do not update them from the output of this
// generator: then the test does not check parity.
//
// dagger/dagger's generator spells a schema before identifiers.CasingVersion
// with golint-style initialisms; this one styles every schema through the
// engine's formatIdentifiers. engineSpellings are the identifiers of the
// golden files that differ, and how the engine spells them.
func TestGenerateCore_Parity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "core", "schema.json"))
	require.NoError(t, err)
	var resp introspection.Response
	require.NoError(t, json.Unmarshal(data, &resp))
	generator.SetSchemaParents(resp.Schema)

	gen := &GoGenerator{Config: generator.Config{Names: styled,
		OutputDir:     t.TempDir(),
		PackageImport: "dagger.io/dagger/core",
		CoreLibrary:   true,
	}}
	state, err := gen.GenerateCore(t.Context(), resp.Schema, resp.SchemaVersion)
	require.NoError(t, err)

	engineSpellings := map[string]string{
		"ParentShas":        "ParentSHAs",
		"Sdks":              "SDKs",
		"Sha":               "SHA",
		"ShortSha":          "ShortSHA",
		"SshfsVolume":       "SSHFSVolume",
		"SshfsVolumeOpts":   "SSHFSVolumeOpts",
		"VcsGeneratedPaths": "VCSGeneratedPaths",
		"VcsIgnoredPaths":   "VCSIgnoredPaths",
	}
	respelled := regexp.MustCompile(`\b(` + strings.Join(slices.Collect(maps.Keys(engineSpellings)), "|") + `)\b`)
	for _, file := range []string{CoreGenFile, DagGenFile} {
		t.Run(file, func(t *testing.T) {
			got, err := fs.ReadFile(state.Overlay, file)
			require.NoError(t, err)
			want, err := os.ReadFile(filepath.Join("testdata", "core", filepath.Base(file)+".golden"))
			require.NoError(t, err)
			want = respelled.ReplaceAllFunc(want, func(name []byte) []byte { return []byte(engineSpellings[string(name)]) })
			require.Equal(t, string(want), string(got))
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
