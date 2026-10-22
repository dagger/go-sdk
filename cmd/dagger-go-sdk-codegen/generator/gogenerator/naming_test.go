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
// optionally with Query.formatIdentifiers, which an engine serves from the
// v1.0.0-beta.21 API view, where identifiers are named by the new rules.
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

func generateCoreFile(t *testing.T, schema *introspection.Schema, schemaVersion string) (string, error) {
	t.Helper()
	gen := &GoGenerator{Config: generator.Config{
		OutputDir:     t.TempDir(),
		PackageImport: "dagger.io/dagger/core",
		CoreLibrary:   true,
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
// naming rules exactly when the schema has Query.formatIdentifiers, and that
// wire names never change.
func TestIdentifierNamingGate(t *testing.T) {
	t.Run("without formatIdentifiers", func(t *testing.T) {
		// The gate is the schema, not the version: a schema without the
		// field keeps the legacy names whatever version it claims.
		got, err := generateCoreFile(t, coreSchema(t, false), "v1.0.0-beta.21")
		require.NoError(t, err)
		want, err := os.ReadFile(filepath.Join("testdata", "core", "core.gen.go.golden"))
		require.NoError(t, err)
		require.Equal(t, string(want), got)
	})

	t.Run("with formatIdentifiers", func(t *testing.T) {
		got, err := generateCoreFile(t, coreSchema(t, true), "v1.0.0-beta.21")
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

	t.Run("schema version past the known naming rules", func(t *testing.T) {
		_, err := generateCoreFile(t, coreSchema(t, true), "v1.0.1")
		require.ErrorContains(t, err, "newer than the identifier naming rules")

		// Without the gate the rules don't apply, so nothing is refused.
		_, err = generateCoreFile(t, coreSchema(t, false), "v1.0.1")
		require.NoError(t, err)
	})
}
