package templates

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeveloperArgEncoding(t *testing.T) {
	tests := map[string]struct {
		typeSpec ParsedType
		want     string
	}{
		"string": {
			typeSpec: &parsedPrimitiveType{goType: types.Typ[types.String]},
			want:     "string",
		},
		"boolean": {
			typeSpec: &parsedPrimitiveType{goType: types.Typ[types.Bool]},
			want:     "json",
		},
		"enum": {
			typeSpec: &parsedEnumTypeReference{},
			want:     "string",
		},
		"core object ID": {
			typeSpec: &parsedObjectTypeReference{},
			want:     "string",
		},
		"module object": {
			typeSpec: &parsedObjectTypeReference{moduleName: "hello"},
			want:     "json",
		},
		"list": {
			typeSpec: &parsedSliceType{},
			want:     "json",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, developerArgEncoding(test.typeSpec))
		})
	}
}

func TestEntrypointContractSurface(t *testing.T) {
	source, err := (&v2Module{}).renderEntrypointSource("hello", "modules/hello", "golang:1.26-alpine")
	require.NoError(t, err)

	text := string(source)
	require.Contains(t, text, "type Entrypoint implements ModuleEntrypoint")
	require.Contains(t, text, "receiverType: String!")
	require.NotContains(t, text, "pub main(")
	require.Contains(t, text, `workspace.findUp("go.mod")`)
	require.Contains(t, text, `.withWorkdir("/workspace/modules/hello")`)
}
