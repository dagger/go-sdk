package templates

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	. "github.com/dave/jennifer/jen" //nolint:staticcheck
	"github.com/stretchr/testify/require"

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
	require.NotContains(t, text, "pub sources(")
	require.Contains(t, text, "pub types(workspace: Workspace!): [TypeDef!]!")
	require.Contains(t, text, "  pub call(\n    workspace: Workspace!,\n    receiverType: String!,")
	require.Contains(t, text, "  pub prebuild: File! {\n    dispatch\n  }")
	require.Contains(t, text, `.withWorkdir("/workspace/" + currentModule.sourceSubpath)`)
	require.Contains(t, text, "fnArgs: JSON!,")
	require.Contains(t, text, "fnArgs: fnArgs,")
	require.Contains(t, text, "(result :: JSON!)")
	require.NotContains(t, text, "FunctionCallArgValue")
}

// The entrypoint builds and runs the module in the images it's given, which
// the SDK pins by digest, and runs it in alpine without one.
func TestEntrypointImages(t *testing.T) {
	const goImage = "docker.io/library/golang:1.26.1-alpine@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const runtimeImage = "docker.io/library/alpine:3.22@sha256:2222222222222222222222222222222222222222222222222222222222222222"
	source, err := (&v2Module{runtimeImage: runtimeImage}).renderEntrypointSource("hello", ".", goImage)
	require.NoError(t, err)
	text := string(source)
	require.Contains(t, text, `.from("`+goImage+`")`)
	require.Contains(t, text, `.from("`+runtimeImage+`")`)
	require.NotContains(t, text, `.from("alpine:3.22")`)

	source, err = (&v2Module{}).renderEntrypointSource("hello", ".", goImage)
	require.NoError(t, err)
	require.Contains(t, string(source), `.from("alpine:3.22")`)
}

// With daggerGo, the entrypoint builds the module's process with dagger/go,
// by its package's workspace directory, instead of a go build of its own.
func TestEntrypointDaggerGoDispatch(t *testing.T) {
	source, err := (&v2Module{daggerGo: true}).renderEntrypointSource("hello", ".", "golang:1.26-alpine", "main")
	require.NoError(t, err)
	text := string(source)
	require.Contains(t, text, "let ws = currentModule.contextDirectory.asWorkspace")
	require.Contains(t, text, "let root = src\n")
	require.Contains(t, text, "let pkg = src\n")
	require.Contains(t, text, "go.module(ws, path: root, findUp: false).packages(ws).get(pkg).binary(ws)")
	require.NotContains(t, text, "go build -o /tmp/dispatch")
	require.NotContains(t, text, "go-sdk-v2-build")

	source, err = (&v2Module{daggerGo: true}).renderEntrypointSource("hello", "modules/hello", "golang:1.26-alpine")
	require.NoError(t, err)
	text = string(source)
	require.Contains(t, text, `let root = if (src == "modules/hello") { "." } else { src.trimSuffix("/modules/hello") }`)
	require.Contains(t, text, `let pkg = if (src == ".") { "cmd/hello-dispatch" } else { src + "/cmd/hello-dispatch" }`)
}

// The entrypoint declares no sources: the manifest includes the files a
// build reads outside the module directory, so every module gets the same
// entrypoint shape.
func TestEntrypointDeclaresNoSources(t *testing.T) {
	source, err := (&v2Module{}).renderEntrypointSource("hello-world", ".", "golang:1.26-alpine")
	require.NoError(t, err)
	text := string(source)
	require.NotContains(t, text, "pub sources(")
	require.NotContains(t, text, "workspace.")
	require.Contains(t, text, "callDigest: String,")
	require.Contains(t, text, `.withEnvVariable("DAGGER_MODULE_CALL_DIGEST", callDigest ?? "")`)
	require.Contains(t, text, `.file("/dagger/result.json")`)
}

// The manifest's include roots each pattern at the anchor, relative to the
// module directory, then excludes every test file outside the module, embedded
// directories' too, and keeps the module's own.
func TestManifestInclude(t *testing.T) {
	require.Nil(t, ManifestInclude(nil))
	require.Nil(t, ManifestInclude(&EntrypointSources{Anchor: ".."}))
	require.Equal(t, []string{
		"../lib/go.mod",
		"../lib/greet/*.go",
		"../lib/greet/templates",
		"!../**/*_test.go",
		"**/*_test.go",
	}, ManifestInclude(&EntrypointSources{Anchor: "..", Include: []string{"lib/go.mod", "lib/greet/*.go", "lib/greet/templates"}}))
	require.Equal(t, []string{
		"../../../go.mod",
		"../../../greet/*.go",
		"!../../../**/*_test.go",
		"**/*_test.go",
	}, ManifestInclude(&EntrypointSources{Anchor: "../../..", Include: []string{"go.mod", "greet/*.go"}}))
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
	mod := &v2Module{objects: []*parsedObjectType{{name: "HelloWorld"}, {name: "Item"}}}
	source, err := mod.renderEntrypointSource("hello-world", ".", "golang:1.26-alpine")
	require.NoError(t, err)
	require.Contains(t, string(source), `.withConstructor(function("", typeDef.withObject("HelloWorld")))`)
	require.Equal(t, 1, strings.Count(string(source), ".withConstructor("))
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

// The entrypoint reads only the last 100 KiB of a failed dispatcher's
// stderr, and the function may have written to it.
func TestDispatchCommandReportsTheErrorLast(t *testing.T) {
	binary := buildDispatchCommand(t, "main", `
import("context";"errors";"fmt";"os";"strings")
func DaggerDispatch(_ context.Context, _ []byte, _, fnName string, _ map[string][]byte) (any, error) {
 fmt.Fprint(os.Stderr, strings.Repeat("a user log line\n", 10000))
 if fnName == "huge" { return nil, errors.New("abc" + strings.Repeat("\u00e9\x01<", 20000)) }
 return nil, errors.New("leaf failed:\n boom")
}
`)
	reported := func(fnName string) (string, string) {
		var stderr strings.Builder
		call := exec.CommandContext(t.Context(), binary, "engine-call", filepath.Join(t.TempDir(), "result.json"))
		call.Stdin = strings.NewReader(`{"receiverType":"Hello","fnName":"` + fnName + `","fnArgs":{}}`)
		call.Stderr = &stderr
		var exitErr *exec.ExitError
		require.ErrorAs(t, call.Run(), &exitErr)
		require.Equal(t, 2, exitErr.ExitCode())
		lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
		last := lines[len(lines)-1]
		require.True(t, strings.HasPrefix(last, functionErrorMarker), last)
		var text string
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(last, functionErrorMarker)), &text))
		return last, text
	}
	_, text := reported("fail")
	require.Equal(t, "leaf failed:\n boom", text)

	last, text := reported("huge")
	require.Less(t, len(last), 100<<10)
	require.True(t, utf8.ValidString(text))
	require.True(t, strings.HasSuffix(text, "..."))
	require.True(t, strings.HasPrefix("abc"+strings.Repeat("\u00e9\x01<", 20000), strings.TrimSuffix(text, "...")))
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

// The engine loads a module that mentions resources through its resources
// path, which v1.0.0-beta.17 has.
func TestEntrypointAvoidsResources(t *testing.T) {
	source, err := entrypointFixture().renderEntrypointSource("hello-world", ".", "golang:1.26.1-alpine", "hello_world")
	require.NoError(t, err)
	require.NotContains(t, string(source), "resources")
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
