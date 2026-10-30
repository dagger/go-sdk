package templates

import (
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	. "github.com/dave/jennifer/jen" //nolint:staticcheck
	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
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
	require.Contains(t, text, "pub sources(workspace: Workspace!): Directory!")
	require.Contains(t, text, "pub types: [TypeDef!]!")
	require.Contains(t, text, `.withWorkdir("/workspace/" + currentModule.sourceSubpath)`)
	require.Contains(t, text, "fnArgs: JSON!,")
	require.Contains(t, text, "fnArgs: fnArgs,")
	require.Contains(t, text, "(result :: JSON!)")
	require.NotContains(t, text, "FunctionCallArgValue")
}

func TestEntrypointWithoutOutsideFilesReturnsItsOwnDirectory(t *testing.T) {
	source, err := (&v2Module{}).renderEntrypointSource("hello-world", ".", "golang:1.26-alpine")
	require.NoError(t, err)
	text := string(source)
	require.Contains(t, text, `  pub sources(workspace: Workspace!): Directory! {
    directory
      .withDirectory(workspace.cwd, workspace.directory(".", exclude: ["**/.git"], gitignore: true))
  }`)
	require.NotContains(t, text, "workspace.findUp")
	require.Contains(t, text, "callDigest: String,")
	require.Contains(t, text, `.withEnvVariable("DAGGER_MODULE_CALL_DIGEST", callDigest ?? "")`)
	require.Contains(t, text, `.file("/dagger/result.json")`)
}

// A module with files outside its directory differs from one without only in
// what sources returns.
func TestEntrypointShapeIsUniform(t *testing.T) {
	render := func(mod *v2Module) (rest, sources string) {
		source, err := mod.renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
		require.NoError(t, err)
		text := string(source)
		start, end := strings.Index(text, "  pub sources("), strings.Index(text, "  let dispatch:")
		require.Positive(t, start)
		require.Greater(t, end, start)
		return text[:start] + text[end:], text[start:end]
	}
	plainRest, plainSources := render(entrypointFixture())
	outsideRest, outsideSources := render(sourcesFixture())
	require.Equal(t, plainRest, outsideRest)
	require.NotEqual(t, plainSources, outsideSources)
	require.NotContains(t, plainRest, "workspace:")
}

// The entrypoint declares the targeted release to the module's process, next
// to the call digest, and declares nothing without one.
func TestEntrypointDeclaresClientVersion(t *testing.T) {
	source, err := (&v2Module{targetVersion: "v1.0.0-beta.21"}).renderEntrypointSource("hello", ".", "golang:1.26-alpine")
	require.NoError(t, err)
	require.Contains(t, string(source), "      .withEnvVariable(\"DAGGER_MODULE_CALL_DIGEST\", callDigest ?? \"\")\n      .withEnvVariable(\"DAGGER_CLIENT_VERSION\", \"v1.0.0-beta.21\")\n      .withExec(\n")
	require.NotContains(t, string(source), "targetVersion")

	source, err = (&v2Module{}).renderEntrypointSource("hello", ".", "golang:1.26-alpine")
	require.NoError(t, err)
	require.Contains(t, string(source), "      .withEnvVariable(\"DAGGER_MODULE_CALL_DIGEST\", callDigest ?? \"\")\n      .withExec(\n")
	require.NotContains(t, string(source), "DAGGER_CLIENT_VERSION")
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
	mod := &v2Module{
		namer:   moduleNamer{naming: identifierstest.Strcase("hello-world")},
		objects: []*parsedObjectType{{name: "HelloWorld"}, {name: "Item"}},
	}
	source, err := mod.renderEntrypointSource("hello-world", ".", "golang:1.26-alpine")
	require.NoError(t, err)
	require.Contains(t, string(source), `.withConstructor(function("", typeDef.withObject("HelloWorld")))`)
	require.Equal(t, 1, strings.Count(string(source), ".withConstructor("))
}

// Generation refuses a collection the engine would refuse at load.
func TestDangObjectRefusesIncompleteCollection(t *testing.T) {
	str := &parsedPrimitiveType{goType: types.Typ[types.String]}
	keys := &fieldSpec{name: "Names", isCollectionKeys: true, typeSpec: &parsedSliceType{underlying: str}}
	lookup := func(marked bool, args ...string) *funcTypeSpec {
		fn := &funcTypeSpec{name: "Lookup", isCollectionGet: marked, returnSpec: str, argSpecs: []paramSpec{{name: "ctx", isContext: true}}}
		for _, arg := range args {
			fn.argSpecs = append(fn.argSpecs, paramSpec{name: arg, typeSpec: str})
		}
		return fn
	}
	collection := func(fields []*fieldSpec, methods ...*funcTypeSpec) *parsedObjectType {
		return &parsedObjectType{name: "Items", isCollection: true, fields: fields, methods: methods}
	}
	_, err := renderDangObject(collection([]*fieldSpec{keys}, lookup(true, "name")))
	require.NoError(t, err)
	_, err = renderDangObject(collection([]*fieldSpec{keys}, lookup(false, "name")))
	require.EqualError(t, err, `collection "Items" requires a stored keys field and a get function`)
	_, err = renderDangObject(collection(nil, lookup(true, "name")))
	require.EqualError(t, err, `collection "Items" requires a stored keys field and a get function`)
	_, err = renderDangObject(collection([]*fieldSpec{keys}, lookup(true, "name", "other")))
	require.EqualError(t, err, `collection "Items" get function requires one argument`)
}

// buildDispatchCommand builds the dispatch command of a module whose
// DaggerDispatch is the fixture, and returns the binary's path.
func buildDispatchCommand(t *testing.T, packageName, fixture string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hello\n\ngo 1.26\n\nrequire github.com/dagger/otel-go v1.43.0\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "module.go"), []byte("package "+packageName+"\n"+fixture), 0644))
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
	tidy := exec.CommandContext(t.Context(), "go", "mod", "tidy")
	tidy.Dir = root
	out, err := tidy.CombinedOutput()
	require.NoError(t, err, string(out))
	binary := filepath.Join(root, "dispatch")
	build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", binary, target)
	build.Dir = root
	out, err = build.CombinedOutput()
	require.NoError(t, err, string(out))
	return binary
}

func TestDispatchCommandSeparatesLogsFromResult(t *testing.T) {
	for _, packageName := range []string{"main", "hello"} {
		t.Run(packageName, func(t *testing.T) {
			binary := buildDispatchCommand(t, packageName, `
import("context";"fmt")
func DaggerDispatch(context.Context, []byte, string, string, map[string][]byte) (any, error) {
 fmt.Println("a user log")
 return map[string]bool{"ok": true}, nil
}
`)
			resultPath := filepath.Join(t.TempDir(), "output", "result.json")
			call := exec.CommandContext(t.Context(), binary, "engine-call", resultPath)
			call.Stdin = strings.NewReader(`{"receiverType":"Hello","fnName":"hi","fnArgs":{}}`)
			out, err := call.CombinedOutput()
			require.NoError(t, err, string(out))
			require.Equal(t, "a user log\n", string(out))
			result, err := os.ReadFile(resultPath)
			require.NoError(t, err)
			require.JSONEq(t, `{"ok":true}`, string(result))
		})
	}
}

func TestDispatchCommandContinuesEngineTrace(t *testing.T) {
	binary := buildDispatchCommand(t, "main", `
import("context";"go.opentelemetry.io/otel/trace")
func DaggerDispatch(ctx context.Context, _ []byte, _, _ string, _ map[string][]byte) (any, error) {
 return trace.SpanContextFromContext(ctx).TraceID().String(), nil
}
`)
	call := exec.CommandContext(t.Context(), binary, "engine-call")
	call.Env = append(os.Environ(), "TRACEPARENT=00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	call.Stdin = strings.NewReader(`{"receiverType":"Hello","fnName":"hi","fnArgs":{}}`)
	out, err := call.Output()
	require.NoError(t, err)
	require.JSONEq(t, `"4bf92f3577b34da6a3ce929d0e0e4736"`, string(out))
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
		targetVersion: "v1.0.0-beta.20",
		namer:         moduleNamer{naming: identifierstest.Strcase("hello-world")},
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

func sourcesFixture() *v2Module {
	mod := entrypointFixture()
	mod.sources = &EntrypointSources{Anchor: "..", Include: []string{"lib/go.mod", "lib/greet/*.go", "lib/greet/greeting.txt"}}
	return mod
}

func TestEntrypointSourcesGolden(t *testing.T) {
	source, err := sourcesFixture().renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
	require.NoError(t, err)
	got := string(source)
	require.Equal(t, updateAndGetFixture(t, "testdata/entrypoint_sources.golden", got), got)
}

func TestEntrypointSourcesAnchor(t *testing.T) {
	mod := entrypointFixture()
	mod.sources = &EntrypointSources{Anchor: "../../..", Include: []string{"go.mod", "greet/*.go"}}
	source, err := mod.renderEntrypointSource("deep", ".", "golang:1.26.1-alpine", "deep")
	require.NoError(t, err)
	require.Contains(t, string(source), `.withDirectory(workspace.cwd + "/../../..", workspace.directory("../../..",
        include: ["go.mod", "greet/*.go"],`)
}

// The engine loads a module that mentions resources through its resources
// path, which v1.0.0-beta.17 has.
func TestEntrypointSourcesAvoidsResources(t *testing.T) {
	for _, mod := range []*v2Module{entrypointFixture(), sourcesFixture()} {
		source, err := mod.renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
		require.NoError(t, err)
		require.NotContains(t, string(source), "resources")
	}
}

func TestExposesModuleContext(t *testing.T) {
	schema := func(fields ...string) *introspection.Schema {
		currentModule := &introspection.Type{Name: "CurrentModule"}
		for _, name := range fields {
			currentModule.Fields = append(currentModule.Fields, &introspection.Field{Name: name})
		}
		return &introspection.Schema{Types: introspection.Types{{Name: "Query"}, currentModule}}
	}
	require.True(t, exposesModuleContext(schema("source", "contextDirectory", "sourceSubpath")))
	require.False(t, exposesModuleContext(schema("source", "contextDirectory")))
	require.False(t, exposesModuleContext(schema("source", "sourceSubpath")))
	require.False(t, exposesModuleContext(&introspection.Schema{Types: introspection.Types{{Name: "Query"}}}))
}

func TestSdkFloorIsTheSourcesFloor(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "versions.toml"))
	require.NoError(t, err)
	match := regexp.MustCompile(`(?m)^sourcesFloor\s*=\s*"([^"]*)"`).FindSubmatch(data)
	require.NotNil(t, match, "sourcesFloor not found in versions.toml")
	require.Equal(t, sourcesFloor, string(match[1]))
}
