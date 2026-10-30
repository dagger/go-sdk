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

// formatted returns what f returns, and checks every name it looked up was
// formatted (identifiers.Names.Err).
func formatted[T any](t *testing.T, names *identifiers.Names, f func() T) T {
	t.Helper()
	got := f()
	require.NoError(t, names.Err())
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
		got := formatted(t, names, func() string { return goNamer{names: names}.formatName(test.name) })
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
	got := formatted(t, names, func() string {
		var buf bytes.Buffer
		require.NoError(t, tmpl.Execute(&buf, object))
		return buf.String()
	})
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
	got := formatted(t, names, func() string {
		var buf bytes.Buffer
		require.NoError(t, tmpl.Execute(&buf, &foo))
		return buf.String()
	})
	require.Contains(t, got, "func (r *Foo) Maybe(ctx context.Context) (*DepGPUThing, error)")
	require.Contains(t, got, `selectNode(q.Root(), *objectID, "DepGpuThing")`)
}

// The module namer predicts the names the engine gives a module's
// declarations: by its rules past the cut, by strcase before.
func TestModuleNamer(t *testing.T) {
	names := recordedNames(t, nil)
	engine := moduleNamer{names: names}
	legacy := moduleNamer{}
	type prediction struct{ legacy, engine string }
	predict := func(t *testing.T, f func(moduleNamer) string) prediction {
		return prediction{
			legacy: f(legacy),
			engine: formatted(t, names, func() string { return f(engine) }),
		}
	}
	for _, test := range []struct {
		what string
		f    func(moduleNamer) string
		want prediction
	}{
		{"object HttpClient", func(n moduleNamer) string { return n.ObjectName("HttpClient") }, prediction{"HttpClient", "HTTPClient"}},
		{"object JsonValue", func(n moduleNamer) string { return n.ObjectName("JsonValue") }, prediction{"JsonValue", "JSONValue"}},
		{"field E2ETest", func(n moduleNamer) string { return n.FieldName("E2ETest") }, prediction{"e2Etest", "e2eTest"}},
		{"field GetUrl", func(n moduleNamer) string { return n.FieldName("GetUrl") }, prediction{"getUrl", "getUrl"}},
		{"constructor naming-app", func(n moduleNamer) string { return n.FieldName("naming-app") }, prediction{"namingApp", "namingApp"}},
		// The module's name prefixes a type on a word boundary only.
		{"type Postman of post", func(n moduleNamer) string { return n.namespaceObject("Postman", "post") }, prediction{"PostPostman", "PostPostman"}},
		{"type PostMan of post", func(n moduleNamer) string { return n.namespaceObject("PostMan", "post") }, prediction{"PostMan", "PostMan"}},
		{"type HttpClient of naming-app", func(n moduleNamer) string { return n.namespaceObject("HttpClient", "naming-app") }, prediction{"NamingAppHttpClient", "NamingAppHTTPClient"}},
		{"type NamingAppHttpClient of naming-app", func(n moduleNamer) string { return n.namespaceObject("NamingAppHttpClient", "naming-app") }, prediction{"NamingAppHttpClient", "NamingAppHTTPClient"}},
		{"type Tools of k8s-tools", func(n moduleNamer) string { return n.namespaceObject("Tools", "k8s-tools") }, prediction{"K8StoolsTools", "K8sToolsTools"}},
		// The engine cuts words around digits and acronyms by the module's
		// spelling too: a flat prefix still counts as the module's name.
		{"type K8STools of k8s-tools", func(n moduleNamer) string { return n.namespaceObject("K8STools", "k8s-tools") }, prediction{"K8StoolsK8Stools", "K8sTools"}},
		{"type My2Go of my2go", func(n moduleNamer) string { return n.namespaceObject("My2Go", "my2go") }, prediction{"My2Go", "My2go"}},
		{"type Go of my2go", func(n moduleNamer) string { return n.namespaceObject("Go", "my2go") }, prediction{"My2GoGo", "My2goGo"}},
	} {
		t.Run(test.what, func(t *testing.T) {
			require.Equal(t, test.want, predict(t, test.f))
		})
	}
}

// The module's main object is the object named like the module, or else the
// only one named like it once flattened (K8STools for k8s-tools).
func TestModuleNamerMainObject(t *testing.T) {
	names := recordedNames(t, nil)
	engine := moduleNamer{names: names}
	isMain := func(n moduleNamer, name, module string, objects ...string) bool {
		return formatted(t, names, func() bool { return n.isMainObject(name, module, objects) })
	}
	require.True(t, isMain(engine, "K8STools", "k8s-tools", "K8STools", "Tools"))
	require.True(t, isMain(engine, "My2Go", "my2go", "My2Go"))
	require.False(t, isMain(engine, "My2Go", "my2go", "My2Go", "My2go"), "an exact match wins over a flat one")
	require.False(t, isMain(engine, "Tools", "k8s-tools", "K8STools", "Tools"))
	require.False(t, moduleNamer{}.isMainObject("K8STools", "k8s-tools", []string{"K8STools"}), "strcase has no flat match")
	require.True(t, moduleNamer{}.isMainObject("K8sTools", "k8s-tools", []string{"K8sTools"}))
	// Only names that are the module's once flattened reach the engine: a
	// helper type the table can't format isn't asked about.
	require.False(t, isMain(engine, "Café", "k8s-tools", "K8STools", "Café"))
	require.True(t, isMain(engine, "K8STools", "k8s-tools", "K8STools", "Café"))
}
