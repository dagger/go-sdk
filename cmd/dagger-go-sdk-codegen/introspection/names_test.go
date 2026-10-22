package introspection

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamesIdentifiers(t *testing.T) {
	with := &Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: Types{{Kind: TypeKindObject, Name: "Query", Fields: []*Field{{Name: "formatIdentifiers"}}}},
	}
	without := &Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: Types{{Kind: TypeKindObject, Name: "Query", Fields: []*Field{{Name: "container"}}}},
	}
	for engine, want := range map[string]bool{
		"v1.0.0-beta.21":          true,
		"1.0.0-beta.21":           true,
		"v1.0.0-beta.21+abcdef12": true,
		"v1.0.0-beta.22":          true,
		"v1.0.0-dev-0123456789ab": true,
		"v1.0.0":                  true,
		"v1.0.0-beta.20":          false,
		"v1.0.0-beta.20+abcdef12": false,
		"v1.0.0-beta.19":          false,
		"v0.19.3-250101-dev-abc":  false,
		"":                        false,
		"devel":                   false,
	} {
		require.Equal(t, want, with.NamesIdentifiers(engine), "engine %q", engine)
		require.False(t, without.NamesIdentifiers(engine), "engine %q without the field", engine)
	}
}

func TestCheckNamingEngine(t *testing.T) {
	for _, tc := range []struct {
		schema, engine string
		refused        bool
	}{
		{"v1.0.0-beta.21", "v1.0.0-beta.20", true},
		{"v1.0.0", "v1.0.0-beta.19", true},
		{"v1.0.0-beta.21", "v1.0.0-beta.21", false},
		{"v1.0.0-beta.20", "v1.0.0-beta.20", false},
		{"v1.0.0-beta.21", "", false},
		{"", "v1.0.0-beta.20", false},
	} {
		err := CheckNamingEngine(tc.schema, tc.engine)
		require.Equal(t, tc.refused, err != nil, "schema %q engine %q: %v", tc.schema, tc.engine, err)
	}
}
