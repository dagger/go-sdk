package gogenerator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// No local SDK replacement supplies a dependency graph here. This is the
// fresh embedded path used by the SDK starter, where tidy previously selected
// an experimental OTel log API incompatible with otel-go's log SDK.
func TestGenerateFreshEmbeddedModuleWithoutLocalSDK(t *testing.T) {
	root := t.TempDir()
	source := "package hello\n\ntype Hello struct{}\nfunc (h *Hello) Echo(value string) string { return value }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
	schema, err := filepath.Abs("../../../cmd/dagger-go-sdk-codegen/generator/gogenerator/testdata/core/schema.json")
	require.NoError(t, err)
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "hello", SchemaPath: schema, SchemaVersion: "v1.0.0-beta.16", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(mod), "github.com/dagger/otel-go v1.43.0")
	cmd := exec.CommandContext(t.Context(), "go", "run", "-buildvcs=false", "./cmd/hello-dispatch", "engine-call")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(`{"receiverType":"Hello","receiverValue":{},"fnName":"Echo","fnArgs":{"value":"fresh published dependencies"}}`)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.JSONEq(t, `"fresh published dependencies"`, strings.TrimSpace(string(out)))
}

func TestGenerateFreshUnifiedModuleWithoutLocalSDK(t *testing.T) {
	const runtimeVersion = "v1.0.0-beta.16.0.20261008202843-134540fec551"
	root := t.TempDir()
	// These private/body-only references are absent from the public module
	// schema, but the default starter still needs their shared core aliases.
	source := `package hello
import (
 "example.com/hello/internal/dagger"
 "example.com/hello/helper"
 "dagger.io/dagger/core"
)
type Hello struct {
 // +private
 Source *dagger.Directory
}
var _ *core.Directory = (*dagger.Directory)(nil)
func New(ws *dagger.Workspace) *Hello {
 return &Hello{Source:ws.Directory("/", dagger.WorkspaceDirectoryOpts{Exclude:[]string{"**/.git"}})}
}
func (h *Hello) Container() *dagger.Container { return dagger.Connect().Container().WithDirectory("/src", h.Source) }
func (h *Hello) Echo(value string) string { return helper.Echo(value) }
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "helper"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "helper", "helper.go"), []byte(`package helper
import client "example.com/hello/internal/dagger"
type privateState struct { secret *client.Secret }
func Echo(value string) string { _ = client.ContainerWithExecOpts{}; return value }
`), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cmd", "probe"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cmd", "probe", "main.go"), []byte(`package main
import "example.com/hello/internal/dagger"
func main() { _ = dagger.ContainerWithEnvVariableOpts{} }
`), 0644))
	for _, dir := range []string{"testdata", ".ignored", "nested-module"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, "fixture.go"), []byte("package fixture\nfunc deliberately invalid syntax"), 0644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested-module", "go.mod"), []byte("module example.com/fixture\n\ngo 1.26.1\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ignored.go"), []byte("//go:build ignore\n\npackage hello\nfunc deliberately invalid syntax"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hello\n\ngo 1.26.1\n"), 0644))
	schema, err := filepath.Abs("../../../cmd/dagger-go-sdk-codegen/generator/gogenerator/testdata/core/schema.json")
	require.NoError(t, err)
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "hello", SchemaPath: schema, SchemaVersion: "v1.0.0", DaggerVersion: runtimeVersion, UnifiedClient: true, GoImage: "golang:1.26-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(mod), "dagger.io/dagger "+runtimeVersion)
	require.NotContains(t, string(mod), "replace")
	bindings, err := os.ReadFile(filepath.Join(root, "internal/dagger/dagger.gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(bindings), "type Directory = core.Directory")
	require.Contains(t, string(bindings), "type WorkspaceDirectoryOpts = core.WorkspaceDirectoryOpts")
	require.Contains(t, string(bindings), "type Secret = core.Secret")
	require.Contains(t, string(bindings), "type ContainerWithExecOpts = core.ContainerWithExecOpts")
	require.Contains(t, string(bindings), "type ContainerWithEnvVariableOpts = core.ContainerWithEnvVariableOpts")
	require.NotContains(t, string(bindings), "var dag ")
	require.NoError(t, Generate(t.Context(), cfg), "published-pin regeneration")
	regenerated, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, string(mod), string(regenerated))
	// A caller's older default must not replace the project's selected newer
	// runtime, even while bootstrap tidy temporarily removes its requirement.
	cfg.DaggerVersion = "v1.0.0-beta.14"
	require.NoError(t, Generate(t.Context(), cfg), "preserve a newer runtime requirement")
	regenerated, err = os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, string(mod), string(regenerated))
	cmd := exec.CommandContext(t.Context(), "go", "run", "-buildvcs=false", "./cmd/hello-dispatch", "engine-call")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(`{"receiverType":"Hello","receiverValue":{},"fnName":"Echo","fnArgs":{"value":"published shared runtime"}}`)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.JSONEq(t, `"published shared runtime"`, strings.TrimSpace(string(out)))
	cmd = exec.CommandContext(t.Context(), "go", "run", "-buildvcs=false", "./cmd/probe")
	cmd.Dir = root
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestGenerateCompleteModule(t *testing.T) {
	sdk := os.Getenv("GO_SDK_TEST_RUNTIME")
	if sdk == "" {
		t.Skip("set GO_SDK_TEST_RUNTIME to the SDK split checkout")
	}
	schema, err := filepath.Abs("../../../cmd/dagger-go-sdk-codegen/generator/gogenerator/testdata/core/schema.json")
	require.NoError(t, err)
	for _, pkg := range []string{"hello", "main"} {
		for _, unified := range []bool{false, true} {
			name := pkg + "/legacy"
			if unified {
				name = pkg + "/unified"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				mod := "module example.com/hello\n\ngo 1.26\n\nrequire dagger.io/dagger v1.0.0-beta.15\nreplace dagger.io/dagger => " + sdk + "\n"
				require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(mod), 0644))
				source := "package " + pkg + `\n
import "example.com/hello/internal/dagger"
type Hello struct{}
type Status string
const Ready Status = "ready"
func (h *Hello) Echo(value *dagger.Container) *dagger.Container { return value }
func (h *Hello) State(status Status) Status { return status }
func (h *Hello) Items() *Items { return &Items{[]string{"a"}} }
// +collection
type Items struct {
 // +keys
 Keys []string
}
// +get
func (items *Items) Lookup(key string) *Item { return &Item{Name:key} }
func (items *Items) Copy() *Items { copied := *items; copied.Keys = []string{"b"}; return &copied }
type Item struct { Name string }
`
				source = strings.Replace(source, `\n`, "\n", 1)
				require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
				cfg := GenerateConfig{ModuleRoot: root, ModuleName: "hello", SchemaPath: schema, SchemaVersion: "v1.0.0-beta.15", UnifiedClient: unified, GoImage: "golang:1.26-alpine"}
				require.NoError(t, Generate(t.Context(), cfg))
				require.NoError(t, Generate(t.Context(), cfg), "regeneration")
				entry, err := os.ReadFile(filepath.Join(root, "internal/dagger/entrypoint/main.dang"))
				require.NoError(t, err)
				require.Contains(t, string(entry), "currentModule.source")
				require.Contains(t, string(entry), ".withCollection\n")
				require.Contains(t, string(entry), `.withCollectionKeys("Keys")`)
				require.Contains(t, string(entry), `.withCollectionGet("Lookup")`)
				require.Contains(t, string(entry), "entrypoint/runtime/main.go.src")
				args := []string{"run", "-buildvcs=false", "./cmd/hello-dispatch", "engine-call"}
				if pkg == "main" {
					args[2] = "."
				}
				cmd := exec.CommandContext(t.Context(), "go", args...)
				cmd.Dir = root
				cmd.Stdin = strings.NewReader(`{"receiverType":"Hello","receiverValue":{},"fnName":"State","fnArgs":{"status":"Ready"}}`)
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, string(out))
				require.JSONEq(t, `"Ready"`, strings.TrimSpace(string(out)))
				original, err := os.ReadFile(filepath.Join(root, "main.go"))
				require.NoError(t, err)
				require.Equal(t, source, string(original))
				// Build exactly the source substitutions used by the Dang entrypoint.
				buildRoot := t.TempDir()
				require.NoError(t, os.CopyFS(buildRoot, os.DirFS(root)))
				files, err := os.ReadDir(filepath.Join(root, "internal/dagger/entrypoint/runtime"))
				require.NoError(t, err)
				for _, file := range files {
					data, err := os.ReadFile(filepath.Join(root, "internal/dagger/entrypoint/runtime", file.Name()))
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(filepath.Join(buildRoot, strings.TrimSuffix(file.Name(), ".src")), data, 0644))
				}
				cmd = exec.CommandContext(t.Context(), "go", args...)
				cmd.Dir = buildRoot
				cmd.Stdin = strings.NewReader(`{"receiverType":"Items","receiverValue":{"Keys":["a"],"__daggerCollectionBase":"original"},"fnName":"Copy","fnArgs":{}}`)
				out, err = cmd.CombinedOutput()
				require.NoError(t, err, string(out))
				require.JSONEq(t, `{"Keys":["b"],"__daggerCollectionBase":"original"}`, strings.TrimSpace(string(out)))
			})
		}
	}
}

func TestGenerateModuleWithIncludes(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod": "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go": "//go:mod:include ../lib/go.mod ../lib/greet !../lib/greet/testdata\n\n" +
			"package app\n\nimport \"example.com/lib/greet\"\n\ntype App struct{}\n\n" +
			"func (*App) Greeting() string { return greet.Greeting() }\n",
		"lib/go.mod":                  "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go":          "package greet\n\nimport _ \"embed\"\n\n//go:embed greeting.txt\nvar greeting string\n\nfunc Greeting() string { return greeting }\n",
		"lib/greet/greeting.txt":      "hello from lib",
		"lib/greet/testdata/data.txt": "not built\n",
	})
	schema, err := filepath.Abs("../../../cmd/dagger-go-sdk-codegen/generator/gogenerator/testdata/core/schema.json")
	require.NoError(t, err)
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "app", SchemaPath: schema, SchemaVersion: "v1.0.0-beta.16", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))

	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), `.directory("..", include: ["lib/go.mod", "lib/greet"], exclude: ["lib/greet/testdata", "app"], gitignore: true)`)
	require.Contains(t, string(entrypoint), `.withDirectory("app", source)`)
	require.Contains(t, string(entrypoint), `.withWorkdir("/workspace/app")`)
	require.Contains(t, string(entrypoint), `.withConstructor(function("", typeDef.withObject("App")).withCachePolicy(FunctionCachePolicy.PerSession))`)
	lib, err := os.ReadFile(filepath.Join(buildRoot, "lib", "greet", "greet.go"))
	require.NoError(t, err)
	require.NotContains(t, string(lib), "Code generated")

	cmd := exec.CommandContext(t.Context(), "go", "run", "-buildvcs=false", "./cmd/app-dispatch", "engine-call")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stdin = strings.NewReader(`{"receiverType":"App","receiverValue":{},"fnName":"Greeting","fnArgs":{}}`)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.JSONEq(t, `"hello from lib"`, strings.TrimSpace(string(out)))

	// A second generation finds nothing to change.
	before, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.NoError(t, Generate(t.Context(), cfg))
	again, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Equal(t, string(entrypoint), string(again))
	after, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}

func TestGenerateCollectionModuleWithIncludes(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod": "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go": "//go:mod:include ../lib/go.mod ../lib/greet\n\n" +
			"package app\n\nimport \"example.com/lib/greet\"\n\ntype App struct{}\n\n" +
			"func (*App) Items() *Items { return &Items{Keys: []string{greet.Greeting()}} }\n\n" +
			"// +collection\ntype Items struct {\n\t// +keys\n\tKeys []string\n}\n\n" +
			"// +get\nfunc (items *Items) Lookup(key string) *Item { return &Item{Name: key} }\n\n" +
			"type Item struct{ Name string }\n",
		"lib/go.mod":         "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go": "package greet\n\nfunc Greeting() string { return \"hello from lib\" }\n",
	})
	schema, err := filepath.Abs("../../../cmd/dagger-go-sdk-codegen/generator/gogenerator/testdata/core/schema.json")
	require.NoError(t, err)
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "app", SchemaPath: schema, SchemaVersion: "v1.0.0-beta.16", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))

	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), `.withFile("main.go", currentModule.source.file("internal/dagger/entrypoint/runtime/main.go.src"))`)
	require.Contains(t, string(entrypoint), `.withDirectory("app", source)`)

	// Build the directory the entrypoint assembles: the included files beside
	// the module, with the runtime sources in place of the author's.
	assembled := t.TempDir()
	require.NoError(t, os.CopyFS(assembled, os.DirFS(buildRoot)))
	runtimeDir := filepath.Join(root, "internal", "dagger", "entrypoint", "runtime")
	files, err := os.ReadDir(runtimeDir)
	require.NoError(t, err)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(runtimeDir, file.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(assembled, "app", strings.TrimSuffix(file.Name(), ".src")), data, 0o644))
	}
	cmd := exec.CommandContext(t.Context(), "go", "run", "-buildvcs=false", "./cmd/app-dispatch", "engine-call")
	cmd.Dir = filepath.Join(assembled, "app")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stdin = strings.NewReader(`{"receiverType":"App","receiverValue":{},"fnName":"Items","fnArgs":{}}`)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "hello from lib")
}

func TestGenerateRefusesUncoveredReplace(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod":  "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go": "package app\n\nimport \"example.com/lib/greet\"\n\ntype App struct{}\n\nvar _ = greet.Greeting\n",
	})
	err := Generate(t.Context(), GenerateConfig{ModuleRoot: root, ModuleName: "app", SchemaPath: "unused"})
	require.EqualError(t, err, "go.mod:7: replace example.com/lib => ../lib reads ../lib/go.mod; add it to //go:mod:include in main.go")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 2, "a refused module changed")
}
