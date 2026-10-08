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
