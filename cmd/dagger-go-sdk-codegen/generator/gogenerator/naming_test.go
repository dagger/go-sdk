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

// coreSchema loads testdata/core/schema.json, a v1.0.0-beta.14 core schema,
// optionally with Query.formatIdentifiers, which engines from v1.0.0-beta.21
// serve at the views where identifiers are named by the new rules, and
// v1.0.0-beta.19 and v1.0.0-beta.20 engines at every v1.0.0 view.
func coreSchema(t *testing.T, formatIdentifiers bool) *introspection.Schema {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "core", "schema.json"))
	require.NoError(t, err)
	var resp introspection.Response
	require.NoError(t, json.Unmarshal(data, &resp))
	require.False(t, resp.Schema.HasFormatIdentifiers())
	if formatIdentifiers {
		query := resp.Schema.Query()
		query.Fields = append(query.Fields, &introspection.Field{
			Name: "formatIdentifiers",
			TypeRef: &introspection.TypeRef{
				Kind: introspection.TypeKindNonNull,
				OfType: &introspection.TypeRef{
					Kind: introspection.TypeKindList,
					OfType: &introspection.TypeRef{
						Kind:   introspection.TypeKindNonNull,
						OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "String"},
					},
				},
			},
		})
		require.True(t, resp.Schema.HasFormatIdentifiers())
	}
	generator.SetSchemaParents(resp.Schema)
	return resp.Schema
}

func generateCoreFile(t *testing.T, schema *introspection.Schema, schemaVersion, engineVersion string) (string, error) {
	t.Helper()
	gen := &GoGenerator{Config: generator.Config{
		OutputDir:     t.TempDir(),
		PackageImport: "dagger.io/dagger/core",
		CoreLibrary:   true,
		EngineVersion: engineVersion,
	}}
	state, err := gen.GenerateCore(t.Context(), schema, schemaVersion)
	if err != nil {
		return "", err
	}
	data, err := fs.ReadFile(state.Overlay, CoreGenFile)
	require.NoError(t, err)
	return string(data), nil
}

// TestIdentifierNamingGate checks that Go identifiers follow the engine's
// naming rules exactly when the schema has Query.formatIdentifiers and the
// engine that served it is v1.0.0-beta.21 or later, and that wire names never
// change.
func TestIdentifierNamingGate(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "core", "core.gen.go.golden"))
	require.NoError(t, err)

	t.Run("without formatIdentifiers", func(t *testing.T) {
		got, err := generateCoreFile(t, coreSchema(t, false), "v1.0.0-beta.21", "v1.0.0-beta.21")
		require.NoError(t, err)
		require.Equal(t, string(golden), got)
	})

	// Released v1.0.0-beta.19 and v1.0.0-beta.20 engines serve the field at
	// every v1.0.0 view, report each of them as v1.0.0, and still name by
	// strcase: the old converters apply, byte for byte.
	for name, engineVersion := range map[string]string{
		"with formatIdentifiers from a v1.0.0-beta.20 engine":       "v1.0.0-beta.20",
		"with formatIdentifiers from a v1.0.0-beta.20+build engine": "v1.0.0-beta.20+abcdef12",
		"with formatIdentifiers from an unknown engine":             "",
	} {
		t.Run(name, func(t *testing.T) {
			schema := coreSchema(t, true)
			got, err := generateCoreFile(t, schema, "v1.0.0", engineVersion)
			require.NoError(t, err)
			require.Contains(t, got, "func (r *GitCommit) ShortSha(ctx context.Context) (string, error) {")
			require.Contains(t, got, "NetworkProtocolTcp NetworkProtocol")
			require.NotContains(t, got, "ShortSHA")

			// The field's own binding aside, the old output exactly.
			withoutField, err := generateCoreFile(t, coreSchema(t, false), "v1.0.0", engineVersion)
			require.NoError(t, err)
			require.Equal(t, string(golden), withoutField)
		})
	}

	for name, engineVersion := range map[string]string{
		"with formatIdentifiers from a v1.0.0-beta.21 engine": "v1.0.0-beta.21",
		"with formatIdentifiers from a dev engine":            "v1.0.0-dev-0123456789ab",
		"with formatIdentifiers from a v1.0.0 engine":         "v1.0.0",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := generateCoreFile(t, coreSchema(t, true), "v1.0.0-beta.21", engineVersion)
			require.NoError(t, err)

			// Methods, enum constants and parameters use the new names.
			require.Contains(t, got, "func (r *GitCommit) ParentSHAs(ctx context.Context) ([]string, error) {")
			require.Contains(t, got, "func (r *GitCommit) ShortSHA(ctx context.Context) (string, error) {")
			require.Contains(t, got, `NetworkProtocolTCP NetworkProtocol = "TCP"`)
			require.Contains(t, got, `FunctionCachePolicyPerSession FunctionCachePolicy = "PerSession"`)
			require.NotContains(t, got, ") ShortSha(")
			require.NotContains(t, got, "NetworkProtocolTcp")

			// Wire names are the schema's.
			require.Contains(t, got, `q := r.query.Select("parentShas")`)
			require.Contains(t, got, `q := r.query.Select("shortSha")`)
		})
	}

	t.Run("schema version past the known naming rules", func(t *testing.T) {
		_, err := generateCoreFile(t, coreSchema(t, true), "v1.1.0", "v1.1.0")
		require.ErrorContains(t, err, "newer than the identifier naming rules")

		// Without the gate the rules don't apply, so nothing is refused.
		_, err = generateCoreFile(t, coreSchema(t, false), "v1.1.0", "v1.1.0")
		require.NoError(t, err)
	})
}
