package gogenerator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// identifierNamingSchema is moduleContextSchema with Query.formatIdentifiers,
// which engines serve from the v1.0.0-beta.21 API view: the view whose
// modules the engine names by engine/naming rather than strcase.
func identifierNamingSchema(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(moduleContextSchema(t))
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(data, &schema))
	for _, typ := range schema["__schema"].(map[string]any)["types"].([]any) {
		typ := typ.(map[string]any)
		if typ["name"] == "Query" {
			typ["fields"] = append(typ["fields"].([]any), map[string]any{
				"name": "formatIdentifiers", "args": []any{},
				"type": map[string]any{"kind": "NON_NULL", "ofType": map[string]any{"kind": "LIST", "ofType": map[string]any{"kind": "NON_NULL", "ofType": map[string]any{"kind": "SCALAR", "name": "String"}}}},
			})
		}
	}
	data, err = json.Marshal(schema)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "schema.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// acronymModule references core types whose names strcase spells otherwise
// (JSONValue, LLMMessage) and declares names with acronyms. Generated from a
// schema the engine names by the new rules, the self-call schema must use the
// engine's names, or the bindings don't build.
var acronymModule = map[string]string{
	"go.mod": "module example.com/app\n\ngo 1.26.1\n",
	"main.go": `package app

import "example.com/app/internal/dagger"

type MyLlmMod struct{}

type HTTPClient struct {
	BaseURL string
}

type Protocol string

const (
	ProtocolHTTP2 Protocol = "HTTP2"
	ProtocolTcpIp Protocol = "tcpIp"
)

func (m *MyLlmMod) Value(value *dagger.JSONValue) *dagger.JSONValue { return value }

func (m *MyLlmMod) Message(message *dagger.LLMMessage) *dagger.LLMMessage { return message }

func (m *MyLlmMod) Client(baseURL string) *HTTPClient { return &HTTPClient{BaseURL: baseURL} }

func (m *MyLlmMod) Protocol(protocol Protocol) Protocol { return protocol }

func (c *HTTPClient) GetURL(path string) string { return c.BaseURL + path }
`,
}

func TestGenerateModuleWithEngineNaming(t *testing.T) {
	root := t.TempDir()
	writeModuleFiles(t, root, acronymModule)
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "my-llm-mod", SchemaPath: identifierNamingSchema(t), SchemaVersion: "v1.0.0-beta.21", EngineVersion: "v1.0.0-beta.21", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
	// Generate compiles the dispatcher, with the self-call bindings.
	require.NoError(t, Generate(t.Context(), cfg))

	// The module's self-call bindings use the engine's names: on the wire,
	// and in Go.
	bindings, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "my-llm-mod.gen.go"))
	require.NoError(t, err)
	for _, want := range []string{
		"type MyLLMModHTTPClient struct",
		`q := r.query.Select("getURL")`,
		`q = q.Arg("baseURL", baseURL)`,
		"func (r *MyLLMModHTTPClient) GetURL(",
		`MyLLMModProtocolTCPIP MyLLMModProtocol = "TCP_IP"`,
		`MyLLMModProtocolHTTP2 MyLLMModProtocol = "HTTP2"`,
	} {
		require.Contains(t, string(bindings), want)
	}
	require.NotContains(t, string(bindings), "Httpclient")
	require.NotContains(t, string(bindings), "Llm")

	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	// The engine names the main object by the new rules: MyLlmMod is the
	// module's main object, so it gets the implicit constructor.
	require.Contains(t, string(entrypoint), `.withConstructor(function("", typeDef.withObject("MyLlmMod")))`)
	// The old spellings of FunctionCachePolicy are valid at every view.
	require.NotContains(t, string(entrypoint), "PER_SESSION")

	cmd := exec.CommandContext(t.Context(), "go", "run", "-buildvcs=false", "./cmd/my-llm-mod-dispatch", "engine-call")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stdin = strings.NewReader(`{"receiverType":"HTTPClient","receiverValue":{"BaseURL":"https://dagger.io"},"fnName":"GetURL","fnArgs":{"path":"/docs"}}`)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.JSONEq(t, `"https://dagger.io/docs"`, strings.TrimSpace(string(out)))
}

func TestGenerateModuleWithLegacyNaming(t *testing.T) {
	for name, tc := range map[string]struct{ schema, schemaVersion, engineVersion string }{
		"without formatIdentifiers": {moduleContextSchema(t), "v1.0.0-beta.20", "v1.0.0-beta.21"},
		// Released v1.0.0-beta.19 and v1.0.0-beta.20 engines serve the field
		// at every v1.0.0 view, report each of them as v1.0.0, and still name
		// modules by strcase.
		"with formatIdentifiers from a v1.0.0-beta.20 engine": {identifierNamingSchema(t), "v1.0.0", "v1.0.0-beta.20"},
		"with formatIdentifiers from an unknown engine":       {identifierNamingSchema(t), "v1.0.0", ""},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			module := map[string]string{
				"go.mod":  acronymModule["go.mod"],
				"main.go": strings.ReplaceAll(strings.ReplaceAll(acronymModule["main.go"], "*dagger.JSONValue", "*dagger.Directory"), "*dagger.LLMMessage", "*dagger.Container"),
			}
			writeModuleFiles(t, root, module)
			cfg := GenerateConfig{ModuleRoot: root, ModuleName: "my-llm-mod", SchemaPath: tc.schema, SchemaVersion: tc.schemaVersion, EngineVersion: tc.engineVersion, DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
			require.NoError(t, Generate(t.Context(), cfg))

			// The names are strcase's, as before.
			bindings, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "my-llm-mod.gen.go"))
			require.NoError(t, err)
			for _, want := range []string{
				"type MyLLMModHttpclient struct",
				`q := r.query.Select("getUrl")`,
				`q = q.Arg("baseUrl", baseUrl)`,
				`MyLlmModProtocolTcpIp MyLlmModProtocol = "TCP_IP"`,
			} {
				require.Contains(t, string(bindings), want)
			}
			core, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "dagger.gen.go"))
			require.NoError(t, err)
			require.Contains(t, string(core), "NetworkProtocolTcp NetworkProtocol")
		})
	}
}

// A unified module aliases core from the published dagger.io/dagger/core,
// whose names predate the new rules: core keeps its names, the module's own
// get the new ones.
func TestGenerateUnifiedModuleWithEngineNaming(t *testing.T) {
	root := t.TempDir()
	writeModuleFiles(t, root, map[string]string{
		"go.mod": acronymModule["go.mod"],
		"main.go": `package app

import "example.com/app/internal/dagger"

type MyLlmMod struct{}

type HTTPClient struct {
	BaseURL string
}

func (m *MyLlmMod) Client(baseURL string) *HTTPClient { return &HTTPClient{BaseURL: baseURL} }

func (m *MyLlmMod) Protocol() dagger.NetworkProtocol { return dagger.NetworkProtocolTcp }

func (m *MyLlmMod) Commit(commit *dagger.GitRef) *dagger.GitRef { return commit }
`,
	})
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "my-llm-mod", SchemaPath: identifierNamingSchema(t), SchemaVersion: "v1.0.0-beta.21", EngineVersion: "v1.0.0-beta.21", DaggerVersion: "v1.0.0-beta.17", UnifiedClient: true, GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))

	bindings, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "my-llm-mod.gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(bindings), "type MyLLMModHTTPClient struct")
	core, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "dagger.gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(core), "const NetworkProtocolTcp = core.NetworkProtocolTcp")
}

// The engine finds a module's main object by its name flattened when the
// type and the module cut words differently around digits (dagger/dagger
// core.Namer.SameDeclaredName): My2Go is module my2go's main object.
func TestGenerateModuleWithFlatMainObject(t *testing.T) {
	root := t.TempDir()
	writeModuleFiles(t, root, map[string]string{
		"go.mod":  "module example.com/my2go\n\ngo 1.26.1\n",
		"main.go": "package my2go\n\ntype My2Go struct{}\n\nfunc (m *My2Go) Hello() string { return \"hi\" }\n",
	})
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "my2go", SchemaPath: identifierNamingSchema(t), SchemaVersion: "v1.0.0-beta.21", EngineVersion: "v1.0.0-dev-0123456789ab", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26.1-alpine"}
	require.NoError(t, Generate(t.Context(), cfg))

	entrypoint, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "entrypoint", "main.dang"))
	require.NoError(t, err)
	require.Contains(t, string(entrypoint), `.withConstructor(function("", typeDef.withObject("My2Go")))`)
	bindings, err := os.ReadFile(filepath.Join(root, "internal", "dagger", "my-2-go.gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(bindings), `XXX_GraphQLType() string {
	return "My2go"
}`)
	require.NotContains(t, string(bindings), "My2goMy2Go")
}
