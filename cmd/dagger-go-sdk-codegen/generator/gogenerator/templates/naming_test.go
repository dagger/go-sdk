package templates

import (
	"bytes"
	"encoding/json"
	"go/format"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// recordedNames are formatted as the engine at v1.0.0-beta.22 (dagger/dagger
// 441d95c2) formats them: testdata/identifiers-v1.0.0-beta.22.json records its
// formatIdentifiers results. A name the table lacks fails the test, unless
// fallback formats it.
func recordedNames(t *testing.T, fallback func(identifiers.Format, string) string) *identifiers.Names {
	t.Helper()
	table, err := identifierstest.Load("testdata/identifiers-v1.0.0-beta.22.json")
	require.NoError(t, err)
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table, Fallback: fallback}
	names, err := engine.Connector().Names(t.Context(), "v1.0.0-beta.22")
	require.NoError(t, err)
	require.NotNil(t, names)
	return names
}

// formatted runs f until every name it looks up in batches is answered (see
// identifiers.Run), and returns what it returned last.
func formatted[T any](t *testing.T, f func() T, batches ...identifiers.Batch) T {
	t.Helper()
	var got T
	require.NoError(t, identifiers.Run(t.Context(), func() error {
		got = f()
		return nil
	}, batches...))
	return got
}

// Past the cut, Go identifiers are the engine's PascalCase with uppercase
// acronyms; before it, the golint-style initialisms stay as they were.
// Internal names keep the legacy spelling either way.
func TestGoNamer(t *testing.T) {
	names := recordedNames(t, nil)
	for _, test := range []struct{ name, legacy, engine string }{
		{"shortSha", "ShortSha", "ShortSHA"},
		{"parentShas", "ParentShas", "ParentSHAs"},
		{"withGpu", "WithGpu", "WithGPU"},
		{"sdks", "Sdks", "SDKs"},
		{"sshfsVolume", "SshfsVolume", "SSHFSVolume"},
		{"asJson", "AsJSON", "AsJSON"},
		{"htmlUrl", "HTMLURL", "HTMLURL"},
		{"httpClient", "HTTPClient", "HTTPClient"},
		{"e2eTest", "E2eTest", "E2ETest"},
		{"LlmMessage", "LLMMessage", "LLMMessage"},
		{"JsonValue", "JSONValue", "JSONValue"},
		{"__typename", "_Typename", "_Typename"},
		{"", "", ""},
	} {
		require.Equal(t, test.legacy, goNamer{}.formatName(test.name), "legacy %q", test.name)
		got := formatted(t, func() string { return goNamer{names: names}.formatName(test.name) }, names)
		require.Equal(t, test.engine, got, "engine %q", test.name)
	}
}

// The bindings spell a field's method and option struct as Go identifiers by
// the engine's rules, and keep its wire name and parameters.
func TestObjectFollowsEngineNames(t *testing.T) {
	schema, object := loadSchemaFromTypeJSON(t, `{
  "kind": "OBJECT",
  "name": "GitCommit",
  "fields": [
    {
      "name": "shortSha",
      "args": [{"name": "length", "type": {"kind": "SCALAR", "name": "Int"}, "defaultValue": null}],
      "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "String"}}
    }
  ]
}`)
	// The table records the names that change; the rest keep their legacy
	// spelling, as the engine leaves them.
	names := recordedNames(t, func(f identifiers.Format, name string) string {
		require.Equal(t, identifiers.GoName, f)
		return legacyFormatName(name)
	})
	funcs := GoTemplateFuncs(schema, nil, "v1.0.0-beta.22", generator.Config{Names: names})
	tmpl, err := template.New("object.go.tmpl").Funcs(funcs).ParseFiles(filepath.Join("src", "_types/object.go.tmpl"))
	require.NoError(t, err)
	got := formatted(t, func() string {
		var buf bytes.Buffer
		require.NoError(t, tmpl.Execute(&buf, object))
		return buf.String()
	}, names)
	source, err := format.Source([]byte("package main\n\n" + got))
	require.NoError(t, err, got)
	got = string(source)
	require.Contains(t, got, "func (r *GitCommit) ShortSHA(ctx context.Context, opts ...GitCommitShortSHAOpts) (string, error)")
	require.Contains(t, got, "type GitCommitShortSHAOpts struct")
	require.Contains(t, got, `Select("shortSha")`)
	require.Contains(t, got, `Arg("length", opts[i].Length)`)
	require.NotContains(t, got, "ShortSha(")
}

// A nullable object is loaded by its schema name, which its Go name can
// spell differently: a dependency named by strcase keeps DepGpuThing, which
// the engine's rules style as DepGPUThing in Go.
func TestNullableObjectLoadsBySchemaName(t *testing.T) {
	var foo, thing introspection.Type
	require.NoError(t, json.Unmarshal([]byte(`{
  "kind": "OBJECT",
  "name": "Foo",
  "fields": [{"name": "maybe", "args": [], "type": {"kind": "OBJECT", "name": "DepGpuThing"}}]
}`), &foo))
	require.NoError(t, json.Unmarshal([]byte(`{
  "kind": "OBJECT",
  "name": "DepGpuThing",
  "fields": [{"name": "id", "args": [], "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}}]
}`), &thing))
	schema := &introspection.Schema{Types: introspection.Types{&foo, &thing}}
	generator.SetSchemaParents(schema)
	generator.SetSchema(schema)
	t.Cleanup(func() { generator.SetSchema(nil) })
	names := recordedNames(t, func(f identifiers.Format, name string) string {
		if name == "DepGpuThing" {
			return "DepGPUThing"
		}
		return legacyFormatName(name)
	})
	funcs := GoTemplateFuncs(schema, nil, "v1.0.0-beta.22", generator.Config{Names: names})
	tmpl, err := template.New("object.go.tmpl").Funcs(funcs).ParseFiles(filepath.Join("src", "_types/object.go.tmpl"))
	require.NoError(t, err)
	got := formatted(t, func() string {
		var buf bytes.Buffer
		require.NoError(t, tmpl.Execute(&buf, &foo))
		return buf.String()
	}, names)
	require.Contains(t, got, "func (r *Foo) Maybe(ctx context.Context) (*DepGPUThing, error)")
	require.Contains(t, got, `selectNode(q.Root(), *objectID, "DepGpuThing")`)
}

// namingApp is how the engine at v1.0.0-beta.22 would answer
// ModuleSource.naming for a module naming-app (Namer.NamespaceObject and
// FieldName in dagger/dagger 441d95c2).
var namingApp = &identifierstest.Module{
	Names: map[string]map[string]string{
		"typeNames":  {"HttpFetcher": "NamingAppHTTPFetcher"},
		"fieldNames": {"GetUrl": "getUrl"},
	},
	Main: func(candidates []string) string { return "NamingApp" },
}

// The module namer gives the engine's names for the module.
func TestModuleNamer(t *testing.T) {
	n := moduleNamer{naming: namingApp}
	require.Equal(t, "NamingAppHTTPFetcher", n.namespaceObject("HttpFetcher"))
	require.Equal(t, "getUrl", n.FieldName("GetUrl"))
	require.True(t, n.isMainObject("NamingApp", []string{"NamingApp", "HttpClient"}))
	require.False(t, n.isMainObject("HttpClient", []string{"NamingApp", "HttpClient"}))
}

// Until the engine answers, a name is a placeholder no schema has, and the
// render runs again with the answer.
func TestModuleNamerPlaceholders(t *testing.T) {
	engine := &identifierstest.Engine{Modules: map[string]*identifierstest.Module{"src": namingApp}}
	naming, err := engine.Connector().ModuleNaming("src")
	require.NoError(t, err)
	n := moduleNamer{naming: naming}
	var rendered []string
	require.NoError(t, identifiers.Run(t.Context(), func() error {
		rendered = append(rendered, n.FieldName("GetUrl"))
		return nil
	}, naming))
	require.Equal(t, []string{"<pending GetUrl>", "getUrl"}, rendered)
}
