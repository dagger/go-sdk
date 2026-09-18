package templates

import (
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	. "github.com/dave/jennifer/jen" //nolint:staticcheck
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
	require.Contains(t, text, "fnArgs: JSON!,")
	require.Contains(t, text, "fnArgs: fnArgs,")
	require.Contains(t, text, "(result :: JSON!)")
	require.NotContains(t, text, "FunctionCallArgValue")
}

func TestEntrypointChecksItsModuleBeforeBuilding(t *testing.T) {
	source, err := (&v2Module{}).renderEntrypointSource("hello-world", ".", "golang:1.26-alpine")
	require.NoError(t, err)

	text := string(source)
	require.Contains(t, text, `found.exists("cmd/hello-world-dispatch/main.go")`)
	require.Contains(t, text, `containsMatch("(?m)^\\s*name\\s*=\\s*[\"']hello-world[\"']\\s*(#.*)?$")`)
	require.Contains(t, text, "git and directory module sources are not supported yet")
	require.Less(t, strings.Index(text, "checkModule(workspace)\n"), strings.Index(text, "goRoot(workspace)\n"))
}

func TestDispatchSourceDecodesArgumentObject(t *testing.T) {
	source, err := (&v2Module{}).renderDispatchSource("hello", "example.com/hello")
	require.NoError(t, err)

	text := string(source)
	require.Contains(t, text, "FnArgs        json.RawMessage `json:\"fnArgs\"`")
	require.Contains(t, text, `jsonValue("fnArgs", req.FnArgs)`)
	require.NotContains(t, text, "callArg")
}

func TestDangFunctionDefaultValueIsJSONText(t *testing.T) {
	source, err := renderDangFunction(&funcTypeSpec{
		name: "Build",
		argSpecs: []paramSpec{{
			name:            "image",
			typeSpec:        &parsedPrimitiveType{goType: types.Typ[types.String]},
			defaultValue:    "alpine:3.21",
			hasDefaultValue: true,
		}},
	})
	require.NoError(t, err)
	require.Contains(t, source, `defaultValue: ("\"alpine:3.21\"" :: Dagger.JSON!)`)
	require.NotContains(t, source, "JSON.decode")
}

func TestDispatchForObjectWithoutFunctions(t *testing.T) {
	source := v2InvokeSrc(map[string][]Code{}, []*parsedObjectType{{name: "Empty"}})
	_, err := parser.ParseFile(token.NewFileSet(), "dispatch.go", "package empty\n\n"+source, 0)
	require.NoError(t, err)
	require.Contains(t, source, `return &Empty{}, nil`)
	require.Contains(t, source, `fmt.Errorf("unknown function %s", fnName)`)
}
