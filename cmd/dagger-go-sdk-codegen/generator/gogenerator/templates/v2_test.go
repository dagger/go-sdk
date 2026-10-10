package templates

import (
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
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
	require.Contains(t, text, "currentModule.source")
	require.Contains(t, text, `.withWorkdir("/workspace")`)
	require.Contains(t, text, "fnArgs: JSON!,")
	require.Contains(t, text, "fnArgs: fnArgs,")
	require.Contains(t, text, "(result :: JSON!)")
	require.NotContains(t, text, "FunctionCallArgValue")
}

func TestEntrypointBuildsOnlyItsOwnSource(t *testing.T) {
	source, err := (&v2Module{}).renderEntrypointSource("hello-world", ".", "golang:1.26-alpine")
	require.NoError(t, err)
	text := string(source)
	require.Contains(t, text, "let source = currentModule.source")
	require.NotContains(t, text, "workspace.directory")
	require.NotContains(t, text, "workspace.findUp")
	require.Contains(t, text, "callDigest: String,")
	require.Contains(t, text, `.withEnvVariable("DAGGER_MODULE_CALL_DIGEST", callDigest ?? "")`)
	require.Contains(t, text, `.file("/dagger/result.json")`)
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

func TestEntrypointExposesDefaultConstructor(t *testing.T) {
	mod := &v2Module{objects: []*parsedObjectType{{name: "HelloWorld"}, {name: "Item"}}}
	source, err := mod.renderEntrypointSource("hello-world", ".", "golang:1.26-alpine")
	require.NoError(t, err)
	require.Contains(t, string(source), `.withConstructor(function("", typeDef.withObject("HelloWorld")))`)
	require.Equal(t, 1, strings.Count(string(source), ".withConstructor("))
}

func TestDispatchCommandSeparatesLogsFromResult(t *testing.T) {
	for _, packageName := range []string{"main", "hello"} {
		t.Run(packageName, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hello\n\ngo 1.26\n"), 0644))
			fixture := "package " + packageName + `
import("context";"fmt")
func DaggerDispatch(context.Context, []byte, string, string, map[string][]byte) (any, error) {
 fmt.Println("a user log")
 return map[string]bool{"ok": true}, nil
}
`
			require.NoError(t, os.WriteFile(filepath.Join(root, "module.go"), []byte(fixture), 0644))
			generated, err := (&v2Module{}).renderDispatchSource("hello", "example.com/hello", packageName)
			require.NoError(t, err)
			dir := root
			target := "."
			if packageName != "main" {
				dir = filepath.Join(root, "cmd", "hello-dispatch")
				target = "./cmd/hello-dispatch"
			}
			require.NoError(t, os.MkdirAll(dir, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "dispatch.go"), generated, 0644))
			binary := filepath.Join(root, "dispatch")
			build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", binary, target)
			build.Dir = root
			out, err := build.CombinedOutput()
			require.NoError(t, err, string(out))
			resultPath := filepath.Join(root, "output", "result.json")
			call := exec.CommandContext(t.Context(), binary, "engine-call", resultPath)
			call.Stdin = strings.NewReader(`{"receiverType":"Hello","fnName":"hi","fnArgs":{}}`)
			out, err = call.CombinedOutput()
			require.NoError(t, err, string(out))
			require.Equal(t, "a user log\n", string(out))
			result, err := os.ReadFile(resultPath)
			require.NoError(t, err)
			require.JSONEq(t, `{"ok":true}`, string(result))
		})
	}
}

// entrypointFixture covers each part of the entrypoint that varies with the
// module: functions with every cache pragma, an implicit constructor, a
// second object, and an enum.
func entrypointFixture() *v2Module {
	str := &parsedPrimitiveType{goType: types.Typ[types.String]}
	fn := func(file, name, cachePolicy string, line int) *funcTypeSpec {
		return &funcTypeSpec{
			name:        name,
			cachePolicy: cachePolicy,
			returnSpec:  str,
			sourceMap:   &sourceMap{filename: file, line: line, column: 1},
		}
	}
	return &v2Module{
		objects: []*parsedObjectType{
			{
				name:      "HelloWorld",
				doc:       "Greets.",
				sourceMap: &sourceMap{filename: "main.go", line: 3, column: 6},
				methods: []*funcTypeSpec{
					fn("main.go", "Hello", "", 5),
					fn("main.go", "Fresh", "never", 8),
					fn("main.go", "Session", "session", 11),
					fn("main.go", "Hourly", "1h", 14),
				},
			},
			{
				name:      "Item",
				sourceMap: &sourceMap{filename: "item.go", line: 3, column: 6},
				methods:   []*funcTypeSpec{fn("item.go", "Name", "", 5)},
			},
		},
		enums: []*parsedEnumType{{
			name:      "Status",
			sourceMap: &sourceMap{filename: "main.go", line: 17, column: 6},
			values:    []*parsedEnumMember{{name: "READY", value: "ready"}},
		}},
	}
}

func TestEntrypointSourceGolden(t *testing.T) {
	source, err := entrypointFixture().renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
	require.NoError(t, err)
	got := string(source)
	require.Equal(t, updateAndGetFixture(t, "testdata/entrypoint.golden", got), got)
}

func includeFixture() *v2Module {
	mod := entrypointFixture()
	main := mod.objects[0]
	main.methods = main.methods[:3]
	mod.include = &EntrypointInclude{
		Anchor:        "..",
		ModulePath:    "hello-world",
		Include:       []string{"lib/go.mod", "lib/greet"},
		Exclude:       []string{"lib/greet/testdata"},
		ModuleExclude: []string{"data"},
	}
	return mod
}

func TestEntrypointIncludeGolden(t *testing.T) {
	source, err := includeFixture().renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
	require.NoError(t, err)
	got := string(source)
	require.Equal(t, updateAndGetFixture(t, "testdata/entrypoint_include.golden", got), got)
}

func TestEntrypointIncludeCachesPerSession(t *testing.T) {
	source, err := includeFixture().renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
	require.NoError(t, err)
	text := string(source)
	// Hello, Session, Name and the constructor; Fresh keeps Never.
	require.Equal(t, 4, strings.Count(text, ".withCachePolicy(FunctionCachePolicy.PerSession)"))
	require.Equal(t, 1, strings.Count(text, ".withCachePolicy(FunctionCachePolicy.Never)"))
	require.NotContains(t, text, "timeToLive")
	require.Contains(t, text, `.withConstructor(function("", typeDef.withObject("HelloWorld")).withCachePolicy(FunctionCachePolicy.PerSession))`)
}

func TestEntrypointIncludeCachesExplicitConstructorPerSession(t *testing.T) {
	mod := includeFixture()
	mod.objects[0].constructor = &funcTypeSpec{
		name:       "New",
		returnSpec: &parsedObjectTypeReference{name: "HelloWorld", moduleName: "hello-world"},
	}
	source, err := mod.renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
	require.NoError(t, err)
	require.Contains(t, string(source), ".withConstructor(\n          function(\"\", typeDef.withObject(\"HelloWorld\"))\n            .withCachePolicy(FunctionCachePolicy.PerSession)\n        )")
}

func TestEntrypointIncludeRefusesTimeToLive(t *testing.T) {
	mod := includeFixture()
	mod.objects[0].methods = entrypointFixture().objects[0].methods
	_, err := mod.renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
	require.EqualError(t, err, `main.go:14:1: +cache="1h" would serve stale results when included files change; use "session" or "never"`)
}

func TestEntrypointIncludeKeepsInterfacePolicies(t *testing.T) {
	mod := includeFixture()
	mod.interfaces = []*parsedIfaceType{{
		name:    "Greeter",
		methods: []*funcTypeSpec{{name: "Greet", cachePolicy: "1h", returnSpec: &parsedPrimitiveType{goType: types.Typ[types.String]}}},
	}}
	source, err := mod.renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
	require.NoError(t, err)
	require.Contains(t, string(source), `timeToLive: "1h"`)
}
