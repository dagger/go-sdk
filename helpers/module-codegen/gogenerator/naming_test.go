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

// The schema's Go identifiers are styled by the engine at the generated
// schema's version, on either side of the cut, while the module's predicted
// schema names, which its interface bindings select, follow the version the
// engine names it by: strcase before the cut. The engine's spellings below
// are what dagger/dagger 441d95c2 formats at v1.0.0-beta.22; it formats
// every version with the same dictionary.
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
	formatted := identifierstest.Table{
		identifierstest.Key(identifiers.FieldName): {"E2ETest": "e2eTest"},
		identifierstest.Key(identifiers.GoName):    {"id": "ID", "shortSha": "ShortSHA", "HttpTester": "HTTPTester"},
	}
	pinned := map[string]identifierstest.Table{"v1.0.0-beta.20": formatted, "v1.0.0-beta.22": formatted}
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
			want:    []string{"func (r *GitCommit) ShortSHA("},
			wantNot: []string{"func (r *GitCommit) ShortSha("},
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

// Unified bindings alias the core of the published runtime, whose Go
// identifiers follow its own version, not the schema's. Here, the schema's
// version is formatted by a fictional later dictionary that spells
// Query.sshfsVolume SshfsVolume: the bindings still spell its option struct
// as the runtime's version does, SSHFSVolumeOpts.
func TestGenerateUnifiedFollowsRuntimeNames(t *testing.T) {
	const runtimeVersion = "v1.0.0-beta.22"
	postCutLibrary(t, runtimeVersion)
	root := t.TempDir()
	source := `package hello
import "example.com/hello/internal/dagger"
type Hello struct{}
func (h *Hello) E2ETest() string { _ = dagger.SSHFSVolumeOpts{}; return "e2e" }
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hello\n\ngo 1.26.1\n"), 0644))
	engine := approximateEngine(map[string]identifierstest.Table{
		runtimeVersion: {
			identifierstest.Key(identifiers.GoName): {"id": "ID", "sshfsVolume": "SSHFSVolume"},
		},
		"v1.0.0-beta.23": {
			identifierstest.Key(identifiers.GoName): {"id": "ID", "sshfsVolume": "SshfsVolume"},
		},
	})
	cfg := GenerateConfig{
		ModuleRoot: root, ModuleName: "hello", SchemaPath: moduleContextSchema(t),
		SchemaVersion: "v1.0.0-beta.23", Connector: engine.Connector(),
		DaggerVersion: runtimeVersion, UnifiedClient: true, GoImage: "golang:1.26-alpine",
	}
	require.NoError(t, Generate(t.Context(), cfg))
	bindings, err := os.ReadFile(filepath.Join(root, "internal/dagger/dagger.gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(bindings), "type SSHFSVolumeOpts = core.SSHFSVolumeOpts")
}

// Unified bindings can't alias the core of a library before the cut, which
// spells its Go identifiers by the old rules: generation refuses it, whether
// targeted or replaced, before it changes anything. A local replacement is an
// SDK developer's, whose core follows the schema.
func TestGenerateUnifiedRefusesPreCutLibrary(t *testing.T) {
	const fix = "so unified bindings can't alias them: "
	for _, test := range []struct {
		name, goMod, target, wantErr string
	}{
		{
			name:    "targeted",
			goMod:   "module example.com/app\n\ngo 1.26.1\n",
			target:  "v1.0.0-beta.21",
			wantErr: `module "app" targets Dagger v1.0.0-beta.21, whose Go library spells its core identifiers by the rules before v1.0.0-beta.22, ` + fix + "set targetVersion to v1.0.0-beta.22 or later in the scope's settings in dagger.toml",
		},
		{
			name:    "replaced",
			goMod:   "module example.com/app\n\ngo 1.26.1\n\nreplace dagger.io/dagger => dagger.io/dagger v1.0.0-beta.17\n",
			target:  "v1.0.0-beta.22",
			wantErr: `module "app" replaces dagger.io/dagger with dagger.io/dagger v1.0.0-beta.17, whose core spells its Go identifiers by the rules before v1.0.0-beta.22, ` + fix + "replace it with v1.0.0-beta.22 or later, or a local checkout past it",
		},
		{
			name:   "replaced for another version",
			goMod:  "module example.com/app\n\ngo 1.26.1\n\nreplace dagger.io/dagger v1.0.0-beta.21 => dagger.io/dagger v1.0.0-beta.17\n",
			target: "v1.0.0-beta.22",
		},
		{
			name:   "replaced locally",
			goMod:  "module example.com/app\n\ngo 1.26.1\n\nreplace dagger.io/dagger => ../sdk\n",
			target: "v1.0.0-beta.21",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			goModPath := filepath.Join(t.TempDir(), "go.mod")
			require.NoError(t, os.WriteFile(goModPath, []byte(test.goMod), 0o644))
			err := checkUnifiedCore(GenerateConfig{DaggerVersion: test.target, UnifiedClient: true}, goModPath, `module "app"`)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, test.wantErr)
			require.NoError(t, checkUnifiedCore(GenerateConfig{DaggerVersion: test.target}, goModPath, `module "app"`), "embedded bindings carry their own core")
		})
	}

	root := t.TempDir()
	files := map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.26.1\n",
		"main.go": "package app\n\ntype App struct{}\n",
	}
	writeModuleFiles(t, root, files)
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "app", SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0", Connector: preCutEngine(), DaggerVersion: "v1.0.0-beta.21", TargetVersion: "v1.0.0-beta.21", UnifiedClient: true, GoImage: "golang:1.26.1-alpine"}
	require.ErrorContains(t, Generate(t.Context(), cfg), `module "app" targets Dagger v1.0.0-beta.21, whose Go library spells its core identifiers by the rules before v1.0.0-beta.22`)
	for name, contents := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, contents, string(data), "a refused module changed")
	}
	require.NoFileExists(t, filepath.Join(root, "dagger.gen.go"), "a refused module changed")
}

// Which Names each side gets, and what an unreadable module version means:
// the schema is styled at its version on any engine, and the module named by
// strcase on an engine that names every module so, while an unreadable
// module version is an error on one that names modules by their version.
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
		{name: "older engine", release: "v1.0.0-beta.21", schema: "v1.0.0", module: "v1.0.0", wantSchema: "v1.0.0", wantModule: "legacy"},
		{name: "unreadable on an older engine", release: "v1.0.0-beta.21", schema: "v1.0.0", moduleErr: errors.New("no such field"), wantSchema: "v1.0.0", wantModule: "legacy"},
		{name: "unreadable past the cut", release: "v1.0.0-beta.22", schema: "v1.0.0-beta.22", moduleErr: errors.New("no such field"), wantErr: "the engine names the module by the version it declares, which couldn't be read: no such field"},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := &identifierstest.Engine{Release: test.release}
			names, moduleNames, err := identifierNames(t.Context(), GenerateConfig{
				SchemaVersion: test.schema, ModuleVersion: test.module, ModuleVersionErr: test.moduleErr, Connector: engine.Connector(),
			}, test.schema)
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
