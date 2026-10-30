package gogenerator

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iancoleman/strcase"
	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

// approximateEngine is an engine past identifiers.CasingVersion that formats
// the names versions pins as recorded there, and every other name roughly
// as the engine would: generated code only needs to compile with them.
func approximateEngine(versions map[string]identifierstest.Table) *identifierstest.Engine {
	return &identifierstest.Engine{
		Release:  "v1.0.0-beta.22",
		Versions: versions,
		Fallback: func(f identifiers.Format, name string) string {
			switch f.Casing {
			case identifiers.Camel:
				return strcase.ToLowerCamel(name)
			case identifiers.Kebab:
				return strcase.ToKebab(name)
			}
			if strings.ContainsAny(name, "-_") {
				return strcase.ToCamel(name)
			}
			return strings.ToUpper(name[:1]) + name[1:]
		},
	}
}

// The schema's Go identifiers follow the generated schema's version, and the
// module's predicted schema names, which its interface bindings select,
// follow the version the engine names it by, whichever of the two is past
// the cut. The engine's spellings below are what dagger/dagger 441d95c2
// formats at v1.0.0-beta.22.
func TestGenerateSplitsNamingVersions(t *testing.T) {
	const source = `package hello

import "context"

type Hello struct{}

type HttpTester interface {
	DaggerObject
	E2ETest(ctx context.Context) (string, error)
}

func (h *Hello) Run(ctx context.Context, tester HttpTester) (string, error) { return tester.E2ETest(ctx) }
`
	pinned := map[string]identifierstest.Table{
		"v1.0.0-beta.22": {
			identifierstest.Key(identifiers.FieldName): {"E2ETest": "e2eTest"},
			identifierstest.Key(identifiers.GoName):    {"id": "ID", "shortSha": "ShortSHA", "HttpTester": "HTTPTester"},
		},
	}
	for _, test := range []struct {
		name           string
		schema, module string
		want, wantNot  []string
		names          []string
	}{
		{
			// A Dang entrypoint without a dang.toml, generated on an engine
			// past the cut.
			name:   "schema past the cut, module before",
			schema: "v1.0.0-beta.22", module: "v1.0.0-beta.20",
			names:   []string{`"HelloHttpTester"`, `"e2Etest"`},
			want:    []string{"func (r *GitCommit) ShortSHA("},
			wantNot: []string{"func (r *GitCommit) ShortSha("},
		},
		{
			name:   "schema before the cut, module past",
			schema: "v1.0.0-beta.20", module: "v1.0.0-beta.22",
			names:   []string{`"HelloHTTPTester"`, `"e2eTest"`},
			want:    []string{"func (r *GitCommit) ShortSha("},
			wantNot: []string{"func (r *GitCommit) ShortSHA("},
		},
		{
			name:   "both past the cut",
			schema: "v1.0.0-beta.22", module: "v1.0.0-beta.22",
			names: []string{`"HelloHTTPTester"`, `"e2eTest"`},
			want:  []string{"func (r *GitCommit) ShortSHA("},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
			engine := approximateEngine(pinned)
			cfg := GenerateConfig{
				ModuleRoot: root, ModuleName: "hello", SchemaPath: moduleContextSchema(t),
				SchemaVersion: test.schema, ModuleVersion: test.module, Connector: engine.Connector(),
				DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26-alpine",
			}
			require.NoError(t, Generate(t.Context(), cfg))
			// The interface bindings load an interface value by its schema
			// name and call its functions by theirs.
			module, err := os.ReadFile(filepath.Join(root, "dagger.gen.go"))
			require.NoError(t, err)
			for _, want := range test.names {
				require.Contains(t, string(module), want)
			}
			core, err := os.ReadFile(filepath.Join(root, "internal/dagger/dagger.gen.go"))
			require.NoError(t, err)
			for _, want := range test.want {
				require.Contains(t, string(core), want)
			}
			for _, unwanted := range test.wantNot {
				require.NotContains(t, string(core), unwanted)
			}
		})
	}
}

// Which Names each side gets, and what an unreadable module version means:
// nothing on an engine that names every module by strcase, an error on one
// that names modules by their version.
func TestIdentifierNames(t *testing.T) {
	version := func(n *identifiers.Names) string {
		if n == nil {
			return "legacy"
		}
		return n.Version()
	}
	for _, test := range []struct {
		name                   string
		release                string
		schema, module         string
		moduleErr              error
		wantSchema, wantModule string
		wantErr                string
	}{
		{name: "module version defaults to the schema's", release: "v1.0.0-beta.22", schema: "v1.0.0-beta.22", wantSchema: "v1.0.0-beta.22", wantModule: "v1.0.0-beta.22"},
		{name: "each its own version", release: "v1.0.0-beta.22", schema: "v1.0.0-beta.23", module: "v1.0.0-beta.22", wantSchema: "v1.0.0-beta.23", wantModule: "v1.0.0-beta.22"},
		{name: "module before the cut", release: "v1.0.0-beta.22", schema: "v1.0.0-beta.22", module: "v1.0.0-beta.20", wantSchema: "v1.0.0-beta.22", wantModule: "legacy"},
		{name: "older engine", release: "v1.0.0-beta.21", schema: "v1.0.0", module: "v1.0.0", wantSchema: "legacy", wantModule: "legacy"},
		{name: "unreadable on an older engine", release: "v1.0.0-beta.21", schema: "v1.0.0", moduleErr: errors.New("no such field"), wantSchema: "legacy", wantModule: "legacy"},
		{name: "unreadable past the cut", release: "v1.0.0-beta.22", schema: "v1.0.0-beta.22", moduleErr: errors.New("no such field"), wantErr: "the engine names the module by the version it declares, which couldn't be read: no such field"},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := &identifierstest.Engine{Release: test.release}
			names, moduleNames, err := identifierNames(t.Context(), GenerateConfig{
				SchemaVersion: test.schema, ModuleVersion: test.module, ModuleVersionErr: test.moduleErr, Connector: engine.Connector(),
			})
			if test.wantErr != "" {
				require.EqualError(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantSchema, version(names))
			require.Equal(t, test.wantModule, version(moduleNames))
		})
	}
}
