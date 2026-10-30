package gogenerator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
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
	schema := moduleContextSchema(t)
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "hello", ModuleSourceID: "hello", Connector: testEngine(), SchemaPath: schema, SchemaVersion: "v1.0.0-beta.16", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26-alpine"}
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
	const runtimeVersion, laterVersion = "v1.0.0-beta.22", "v1.0.0-beta.23"
	postCutLibrary(t, runtimeVersion, laterVersion)
	root := t.TempDir()
	// These private/body-only references are absent from the public module
	// schema, but the default starter still needs their shared core aliases.
	source := `package hello
import (
 moduleClient "example.com/hello/internal/dagger"
 "dagger.io/dagger"
 helperClient "example.com/hello/helper"
 "dagger.io/dagger/core"
)
type Hello struct {
 // +private
 Source *moduleClient.Directory
 // +private
 State *helperClient.State
}
var _ *core.Directory = (*moduleClient.Directory)(nil)
func New(ws *moduleClient.Workspace) *Hello {
 return &Hello{Source:ws.Directory("/", moduleClient.WorkspaceDirectoryOpts{Exclude:[]string{"**/.git"}}), State:&helperClient.State{Value:"private"}}
}
func (h *Hello) Container() *moduleClient.Container { return moduleClient.Connect().Container().WithDirectory("/src", h.Source) }
func (h *Hello) Echo(value string) string { return helperClient.Echo(value) }
func (h *Hello) Base() *moduleClient.Container { return core.NewContainer().WithDirectory("/src", h.Source) }
func (*Hello) RuntimeError() string { return dagger.ErrClientClosed.Error() }
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "helper"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "helper", "helper.go"), []byte(`package helper
import client "example.com/hello/internal/dagger"
type privateState struct { secret *client.Secret }
type State struct { Value string }
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
	schema := moduleContextSchema(t)
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "hello", ModuleSourceID: "hello", SchemaPath: schema, SchemaVersion: "v1.0.0", Connector: testEngine(), DaggerVersion: runtimeVersion, UnifiedClient: true, GoImage: "golang:1.26-alpine"}
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
	// A module calls itself through a client it adds, like any dependency.
	require.NoFileExists(t, filepath.Join(root, "internal/dagger/hello.gen.go"))
	require.NoError(t, Generate(t.Context(), cfg), "published-pin regeneration")
	regenerated, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, string(mod), string(regenerated))
	// The library follows the target in both directions, even while the
	// bootstrap tidy temporarily removes its requirement.
	cfg.DaggerVersion = laterVersion
	require.NoError(t, Generate(t.Context(), cfg), "raise the target")
	regenerated, err = os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(regenerated), "dagger.io/dagger "+laterVersion)
	cfg.DaggerVersion = runtimeVersion
	require.NoError(t, Generate(t.Context(), cfg), "lower the target")
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
	schema := moduleContextSchema(t)
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
				cfg := GenerateConfig{ModuleRoot: root, ModuleName: "hello", ModuleSourceID: "hello", Connector: testEngine(), SchemaPath: schema, SchemaVersion: "v1.0.0-beta.15", UnifiedClient: unified, GoImage: "golang:1.26-alpine"}
				require.NoError(t, Generate(t.Context(), cfg))
				require.NoError(t, Generate(t.Context(), cfg), "regeneration")
				entry, err := os.ReadFile(filepath.Join(root, "internal/dagger/entrypoint/main.dang"))
				require.NoError(t, err)
				require.Contains(t, string(entry), "let collectionContext: Directory! {\n    currentModule.contextDirectory\n")
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

func TestGenerateModuleWithSources(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod": "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go": "package app\n\nimport \"example.com/lib/greet\"\n\ntype App struct{}\n\n" +
			"func (*App) Greeting() string { return greet.Greeting() }\n",
		"lib/go.mod":                  "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go":          "package greet\n\nimport _ \"embed\"\n\n//go:embed greeting.txt\nvar greeting string\n\nfunc Greeting() string { return greeting }\n",
		"lib/greet/greet.h":           "",
		"lib/greet/greeting.txt":      "hello from lib",
		"lib/greet/testdata/data.txt": "not built\n",
	})
	cfg := GenerateConfig{BuildRoot: buildRoot, ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", Connector: testEngine(), SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0-beta.17", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))

	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), `.withDirectory(workspace.cwd + "/..", workspace.directory("..",
        include: ["lib/go.mod", "lib/greet/*.go", "lib/greet/*.h", "lib/greet/*.s", "lib/greet/*.syso", "lib/greet/greeting.txt"],`)
	require.Contains(t, string(entrypoint), `.withMountedDirectory("/workspace", currentModule.contextDirectory)`)
	require.NotContains(t, string(entrypoint), "let source")
	require.NotContains(t, string(entrypoint), "currentModule.source.")
	require.NoDirExists(t, filepath.Join(root, "internal", "dagger", "entrypoint", "go-inputs"))
	lib, err := os.ReadFile(filepath.Join(buildRoot, "lib", "greet", "greet.go"))
	require.NoError(t, err)
	require.NotContains(t, string(lib), "Code generated")
	moduleSource, err := os.ReadFile(filepath.Join(root, "dagger.gen.go"))
	require.NoError(t, err)
	require.NotContains(t, string(moduleSource), "compilationWorkspace")

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

func TestGenerateSourcesMatchTidiedGoMod(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod":         "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go":        "package app\n\ntype App struct{}\n\nfunc (*App) Hello() string { return \"hello\" }\n",
		"lib/go.mod":         "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go": "package greet\n",
	})
	cfg := GenerateConfig{BuildRoot: buildRoot, ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", Connector: testEngine(), SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0-beta.17", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))

	entrypointPath := filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang")
	entrypoint, err := os.ReadFile(entrypointPath)
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), `  pub sources(workspace: Workspace!): Directory! {
    directory
      .withDirectory(workspace.cwd, workspace.directory(".", exclude: ["**/.git"], gitignore: true))
  }
`)
	require.NotContains(t, string(entrypoint), "warning")
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.NotContains(t, string(goMod), "example.com/lib v0.0.0")

	require.NoError(t, Generate(t.Context(), cfg))
	again, err := os.ReadFile(entrypointPath)
	require.NoError(t, err)
	require.Equal(t, string(entrypoint), string(again))
	goModAgain, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, string(goMod), string(goModAgain))
}

func TestGenerateModuleEmbeddingGoFiles(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod":                 "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go":                "package app\n\nimport \"example.com/lib/greet\"\n\ntype App struct{}\n\nvar _ = greet.Assets\n",
		"lib/go.mod":                 "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go":         "package greet\n\nimport \"embed\"\n\n//go:embed assets/*.go\nvar Assets embed.FS\n",
		"lib/greet/assets/asset.go":  "package assets\n",
		"lib/greet/assets/private.h": "",
	})
	cfg := GenerateConfig{BuildRoot: buildRoot, ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", Connector: testEngine(), SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0-beta.17", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))

	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint),
		`include: ["lib/go.mod", "lib/greet/*.go", "lib/greet/*.h", "lib/greet/*.s", "lib/greet/*.syso", "lib/greet/assets/*.go"],`)
}

func TestGenerateCollectionModuleWithSources(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod": "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go": "package app\n\nimport \"example.com/lib/greet\"\n\ntype App struct{}\n\n" +
			"func (*App) Items() *Items { return &Items{Keys: []string{greet.Greeting()}} }\n\n" +
			"// +collection\ntype Items struct {\n\t// +keys\n\tKeys []string\n}\n\n" +
			"// +get\nfunc (items *Items) Lookup(key string) *Item { return &Item{Name: key} }\n\n" +
			"type Item struct{ Name string }\n",
		"lib/go.mod":         "module example.com/lib\n\ngo 1.26.1\n",
		"lib/greet/greet.go": "package greet\n\nfunc Greeting() string { return \"hello from lib\" }\n",
	})
	cfg := GenerateConfig{BuildRoot: buildRoot, ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", Connector: testEngine(), SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0-beta.17", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))

	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), `.withFile(currentModule.sourceSubpath + "/main.go", currentModule.contextDirectory.file(currentModule.sourceSubpath + "/internal/dagger/entrypoint/runtime/main.go.src"))`)
	require.Contains(t, string(entrypoint), `.withMountedDirectory("/workspace", collectionContext)`)
	require.NotContains(t, string(entrypoint), "let source")
	require.NotContains(t, string(entrypoint), "currentModule.source.")
	require.Contains(t, string(entrypoint), `.withCollectionGet("Lookup")`)

	// Build the directory the entrypoint mounts: the included files beside the
	// module, with the runtime sources in place of the author's.
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
	require.NotContains(t, string(out), "__daggerCompilationWorkspace")
}

// Unified bindings do not import otel-go, so its requirement is gone when the
// collection runtime loads package main, whose dispatcher imports it.
func TestGenerateUnifiedCollectionModule(t *testing.T) {
	postCutLibrary(t, "v1.0.0-beta.22")
	root := t.TempDir()
	writeModuleFiles(t, root, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26.1\n",
		"main.go": "package main\n\ntype App struct{}\n\n" +
			"func (*App) Items() *Items { return &Items{Keys: []string{\"first\"}} }\n\n" +
			"// +collection\ntype Items struct {\n\t// +keys\n\tKeys []string\n}\n\n" +
			"// +get\nfunc (items *Items) Lookup(key string) *Item { return &Item{Name: key} }\n\n" +
			"type Item struct{ Name string }\n",
	})
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0", Connector: testEngine(), DaggerVersion: "v1.0.0-beta.22", UnifiedClient: true, GoImage: "golang:1.26-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))
	dispatcher, err := os.ReadFile(filepath.Join(root, "dagger.dispatch.gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(dispatcher), `"github.com/dagger/otel-go"`)
	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), `.withMountedDirectory("/workspace", collectionContext)`)
}

// Workspace compilation vendored a discovery tool into the entrypoint
// directory; generating the module again removes it.
func TestGenerateRemovesVendoredDiscoveryTool(t *testing.T) {
	root := t.TempDir()
	tool := map[string]string{
		"internal/dagger/entrypoint/go-inputs/go.mod":  "module go-inputs\n\ngo 1.26\n",
		"internal/dagger/entrypoint/go-inputs/main.go": "// Code generated by dagger. DO NOT EDIT.\n\npackage main\n\nfunc main() {}\n",
	}
	owned := map[string]string{}
	for path, contents := range tool {
		sum := sha256.Sum256([]byte(contents))
		owned[path] = hex.EncodeToString(sum[:])
	}
	ownership, err := json.Marshal(map[string]any{"version": 1, "files": owned})
	require.NoError(t, err)
	files := map[string]string{
		"go.mod":                 "module example.com/app\n\ngo 1.26.1\n",
		"main.go":                "package app\n\ntype App struct{}\n\nfunc (*App) Hello() string { return \"hello\" }\n",
		".dagger-generated.json": string(ownership),
	}
	for path, contents := range tool {
		files[path] = contents
	}
	writeModuleFiles(t, root, files)
	schema := moduleContextSchema(t)
	removedList := filepath.Join(t.TempDir(), "removed.txt")
	require.NoError(t, Generate(t.Context(), GenerateConfig{ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", Connector: testEngine(), SchemaPath: schema, SchemaVersion: "v1.0.0-beta.16", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine", RemovedPath: removedList}))

	removed, err := os.ReadFile(removedList)
	require.NoError(t, err)
	for path := range tool {
		require.NoFileExists(t, filepath.Join(root, path))
		require.Contains(t, string(removed), path+"\n")
	}
}

func TestGenerateRefusesEngineWithoutModuleContext(t *testing.T) {
	root := t.TempDir()
	writeModuleFiles(t, root, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.26.1\n",
		"main.go": "package app\n\ntype App struct{}\n",
	})
	schema, err := filepath.Abs("../../../cmd/dagger-go-sdk-codegen/generator/gogenerator/testdata/core/schema.json")
	require.NoError(t, err)
	err = Generate(t.Context(), GenerateConfig{ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", Connector: testEngine(), SchemaPath: schema, SchemaVersion: "v1.0.0-beta.16", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"})
	require.ErrorContains(t, err, "the generated Dang entrypoint needs Dagger v1.0.0-beta.22 or later")
}

// moduleContextSchema is the core schema with the CurrentModule fields of an
// engine that exposes the module context.
func moduleContextSchema(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../../cmd/dagger-go-sdk-codegen/generator/gogenerator/testdata/core/schema.json")
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(data, &schema))
	for _, typ := range schema["__schema"].(map[string]any)["types"].([]any) {
		typ := typ.(map[string]any)
		if typ["name"] == "CurrentModule" {
			typ["fields"] = append(typ["fields"].([]any),
				map[string]any{"name": "contextDirectory", "args": []any{}, "type": map[string]any{"kind": "NON_NULL", "ofType": map[string]any{"kind": "OBJECT", "name": "Directory"}}},
				map[string]any{"name": "sourceSubpath", "args": []any{}, "type": map[string]any{"kind": "NON_NULL", "ofType": map[string]any{"kind": "SCALAR", "name": "String"}}},
			)
		}
	}
	data, err = json.Marshal(schema)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "schema.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func TestGenerateRefusesMissingReplaceTarget(t *testing.T) {
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod":  "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go": "package app\n\nimport \"example.com/lib/greet\"\n\ntype App struct{}\n\nvar _ = greet.Greeting\n",
	})
	err := Generate(t.Context(), GenerateConfig{ModuleRoot: root, ModuleName: "app", SchemaPath: "unused"})
	require.EqualError(t, err, "go.mod:7: replace example.com/lib => ../lib reads ../lib/go.mod, which is missing")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 2, "a refused module changed")
}

func writeModuleFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0644))
	}
}

// The entrypoint declares the release the generated code targets to the
// module's process, and declares nothing without one.
func TestGenerateEntrypointDeclaresClientVersion(t *testing.T) {
	root := t.TempDir()
	writeModuleFiles(t, root, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.26.1\n",
		"main.go": "package app\n\ntype App struct{}\n\nfunc (*App) Hello() string { return \"hello\" }\n",
	})
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", Connector: testEngine(), SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0-beta.17", DaggerVersion: "v1.0.0-beta.14", TargetVersion: "v1.0.0-beta.20", GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))
	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), "\n      .withEnvVariable(\"DAGGER_MODULE_CALL_DIGEST\", callDigest ?? \"\")\n      .withEnvVariable(\"DAGGER_CLIENT_VERSION\", \"v1.0.0-beta.20\")\n      .withExec(\n")

	cfg.TargetVersion = ""
	require.NoError(t, Generate(t.Context(), cfg))
	entrypoint, err = os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.NotContains(t, string(entrypoint), "DAGGER_CLIENT_VERSION")
}

// A go.mod requirement that needs a later library than the target is
// refused with the requirement named, before anything is compiled.
func TestGenerateUnifiedRefusesLibraryRaisedByRequirement(t *testing.T) {
	postCutLibrary(t, "v1.0.0-beta.22", "v1.0.0-beta.23")
	buildRoot := t.TempDir()
	root := filepath.Join(buildRoot, "app")
	writeModuleFiles(t, buildRoot, map[string]string{
		"app/go.mod":         "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n",
		"app/main.go":        "package app\n\nimport \"example.com/lib/greet\"\n\ntype App struct{}\n\nfunc (*App) Greeting() string { return greet.Greeting() }\n",
		"lib/go.mod":         "module example.com/lib\n\ngo 1.26.1\n\nrequire dagger.io/dagger v1.0.0-beta.23\n",
		"lib/greet/greet.go": "package greet\n\nfunc Greeting() string { return \"hello\" }\n",
	})
	cfg := GenerateConfig{BuildRoot: buildRoot, ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0", Connector: testEngine(), DaggerVersion: "v1.0.0-beta.22", TargetVersion: "v1.0.0-beta.22", UnifiedClient: true, GoImage: "golang:1.26.1-alpine"}
	err := Generate(t.Context(), cfg)
	const refusal = `module "app" targets Dagger v1.0.0-beta.22, and example.com/lib v0.0.0 requires dagger.io/dagger v1.0.0-beta.23: set targetVersion to v1.0.0-beta.23 or later in the scope's settings in dagger.toml, or require a version of example.com/lib that needs dagger.io/dagger v1.0.0-beta.22 or earlier`
	require.EqualError(t, err, refusal)
	require.NoFileExists(t, filepath.Join(root, "dagger.gen.go"), "a refused module changed")

	// A replace directive for a version the build does not select grants no
	// development exception.
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n\nreplace dagger.io/dagger v1.0.0-beta.16 => dagger.io/dagger v1.0.0-beta.15\n")
	require.EqualError(t, Generate(t.Context(), cfg), refusal)

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.26.1\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n\nreplace dagger.io/dagger v1.0.0-beta.22 => dagger.io/dagger v1.0.0-beta.22\n")
	require.EqualError(t, Generate(t.Context(), cfg), refusal)

	cfg.DaggerVersion, cfg.TargetVersion = "v1.0.0-beta.23", "v1.0.0-beta.23"
	require.NoError(t, Generate(t.Context(), cfg))
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "dagger.io/dagger v1.0.0-beta.23")
}

func TestGenerateUnifiedRefusesUnpublishedTarget(t *testing.T) {
	proxyURL := url.URL{Scheme: "file", Path: filepath.ToSlash(t.TempDir())}
	t.Setenv("GOPROXY", proxyURL.String())
	t.Setenv("GONOPROXY", "none")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOWORK", "off")

	root := t.TempDir()
	writeModuleFiles(t, root, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.26.1\n",
		"main.go": "package app\n\ntype App struct{}\n",
	})
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0", Connector: testEngine(), DaggerVersion: "v1.0.0-beta.99", TargetVersion: "v1.0.0-beta.99", UnifiedClient: true, GoImage: "golang:1.26.1-alpine"}
	err := Generate(t.Context(), cfg)
	require.ErrorContains(t, err, `module "app" targets Dagger v1.0.0-beta.99, and its Go library dagger.io/dagger v1.0.0-beta.99 cannot be resolved (`)
	require.ErrorContains(t, err, `): set targetVersion to a release whose library is published in the scope's settings in dagger.toml, or replace dagger.io/dagger in go.mod for development`)
	require.NoFileExists(t, filepath.Join(root, "dagger.gen.go"), "a refused module changed")
}

// A replaced library is a development exception: generation reports it,
// keeps the replace directive, and does not pin the library to the target.
func TestGenerateUnifiedReportsReplacedLibrary(t *testing.T) {
	postCutLibrary(t, "v1.0.0-beta.22")
	root := t.TempDir()
	const goMod = "module example.com/app\n\ngo 1.26.1\n\nrequire dagger.io/dagger v1.0.0-beta.16\n\nreplace dagger.io/dagger => dagger.io/dagger v1.0.0-beta.22\n"
	writeModuleFiles(t, root, map[string]string{
		"go.mod":  goMod,
		"main.go": "package app\n\ntype App struct{}\n\nfunc (*App) Hello() string { return \"hello\" }\n",
	})
	var notes strings.Builder
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "app", ModuleSourceID: "app", SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0", Connector: testEngine(), DaggerVersion: "v1.0.0-beta.22", TargetVersion: "v1.0.0-beta.22", UnifiedClient: true, GoImage: "golang:1.26.1-alpine", Notes: &notes}
	require.NoError(t, Generate(t.Context(), cfg))
	require.Equal(t, `warning: module "app" targets Dagger v1.0.0-beta.22, and go.mod replaces dagger.io/dagger: the generated code uses the replacement's API, which that release does not guarantee; this is a development exception, so remove the replace directive before publishing`+"\n", notes.String())
	regenerated, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.NotContains(t, string(regenerated), "\tdagger.io/dagger v1.0.0-beta.22\n", "the library was pinned to the target")
	require.NotContains(t, string(regenerated), "require dagger.io/dagger v1.0.0-beta.22\n", "the library was pinned to the target")
	require.Contains(t, string(regenerated), "replace dagger.io/dagger => dagger.io/dagger v1.0.0-beta.22")
	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), "\n      .withEnvVariable(\"DAGGER_CLIENT_VERSION\", \"v1.0.0-beta.22\")\n")
}
