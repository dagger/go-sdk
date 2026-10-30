package gogenerator

import (
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
// as the engine would: generated code only needs to compile with them. It
// serves the naming of module source "src" from module.
func approximateEngine(versions map[string]identifierstest.Table, module *identifierstest.Module) *identifierstest.Engine {
	return &identifierstest.Engine{
		Release:  "v1.0.0-beta.22",
		Versions: versions,
		Modules:  map[string]*identifierstest.Module{"src": module},
		Fallback: func(f identifiers.Format, name string) string {
			switch f.Casing {
			case identifiers.Camel:
				return strcase.ToLowerCamel(name)
			case identifiers.ScreamingSnake:
				return strcase.ToScreamingSnake(name)
			}
			if strings.ContainsAny(name, "-_") {
				return strcase.ToCamel(name)
			}
			return strings.ToUpper(name[:1]) + name[1:]
		},
	}
}

// helloModule is how the engine would answer ModuleSource.naming for module
// hello: the names pins records, and strcase for the rest, which the
// generated code only needs to compile with.
func helloModule(pins map[string]map[string]string) *identifierstest.Module {
	mod := identifierstest.Strcase("hello")
	mod.Names = pins
	return mod
}

// The schema's Go identifiers follow the generated schema's version, and the
// module's schema names are the engine's naming of it, whichever side of the
// cut either is on. The engine's spellings below are what dagger/dagger
// 441d95c2 names at v1.0.0-beta.22.
func TestGenerateSplitsNamingVersions(t *testing.T) {
	const source = `package hello

type Hello struct{}

type Arch string

const ArchArm64 Arch = "arm64"

func (h *Hello) E2ETest() string { return "e2e" }

func (h *Hello) Platform(arch Arch) string { return string(arch) }
`
	pinned := map[string]identifierstest.Table{
		"v1.0.0-beta.22": {
			identifierstest.Key(identifiers.GoName): {"id": "ID", "e2eTest": "E2ETest", "e2Etest": "E2ETest", "shortSha": "ShortSHA"},
		},
	}
	modules := map[bool]*identifierstest.Module{
		true: helloModule(nil),
		false: helloModule(map[string]map[string]string{
			"fieldNames": {"E2ETest": "e2eTest"},
			// The member's name is its constant's, less the type's name.
			"enumMemberNames": {"Arm64": "ARM64"},
		}),
	}
	for _, test := range []struct {
		name          string
		schema        string
		legacy        bool
		want, wantNot []string
		self          []string
	}{
		{
			// A Dang entrypoint without a dang.toml, generated on an engine
			// past the cut.
			name:   "schema past the cut, module before",
			schema: "v1.0.0-beta.22", legacy: true,
			self:    []string{`Select("e2Etest")`, `= "ARM_64"`, "func (r *Hello) E2ETest("},
			want:    []string{"func (r *GitCommit) ShortSHA("},
			wantNot: []string{"func (r *GitCommit) ShortSha("},
		},
		{
			name:   "schema before the cut, module past",
			schema: "v1.0.0-beta.20", legacy: false,
			self:    []string{`Select("e2eTest")`, `= "ARM64"`, "func (r *Hello) E2eTest("},
			want:    []string{"func (r *GitCommit) ShortSha("},
			wantNot: []string{"func (r *GitCommit) ShortSHA("},
		},
		{
			name:   "both past the cut",
			schema: "v1.0.0-beta.22", legacy: false,
			self: []string{`Select("e2eTest")`, `= "ARM64"`, "func (r *Hello) E2ETest("},
			want: []string{"func (r *GitCommit) ShortSHA("},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
			engine := approximateEngine(pinned, modules[test.legacy])
			cfg := GenerateConfig{
				ModuleRoot: root, ModuleName: "hello", SchemaPath: moduleContextSchema(t),
				SchemaVersion: test.schema, ModuleSourceID: "src", Connector: engine.Connector(),
				DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26-alpine",
			}
			require.NoError(t, Generate(t.Context(), cfg))
			self, err := os.ReadFile(filepath.Join(root, "internal/dagger/hello.gen.go"))
			require.NoError(t, err)
			for _, want := range test.self {
				require.Contains(t, string(self), want)
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
// identifiers follow its own version, not the schema's: before the cut, the
// option struct of Query.sshfsVolume is SshfsVolumeOpts, however the schema
// past the cut would spell it. The module's schema names are still the
// engine's naming of it.
func TestGenerateUnifiedFollowsRuntimeNames(t *testing.T) {
	const runtimeVersion = "v1.0.0-beta.17"
	root := t.TempDir()
	source := `package hello
import "example.com/hello/internal/dagger"
type Hello struct{}
func (h *Hello) E2ETest() string { _ = dagger.SshfsVolumeOpts{}; return "e2e" }
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hello\n\ngo 1.26.1\n"), 0644))
	engine := approximateEngine(map[string]identifierstest.Table{
		"v1.0.0-beta.22": {
			identifierstest.Key(identifiers.GoName): {"id": "ID", "sshfsVolume": "SSHFSVolume"},
		},
	}, helloModule(map[string]map[string]string{"fieldNames": {"E2ETest": "e2eTest"}}))
	cfg := GenerateConfig{
		ModuleRoot: root, ModuleName: "hello", SchemaPath: moduleContextSchema(t),
		SchemaVersion: "v1.0.0-beta.22", ModuleSourceID: "src", Connector: engine.Connector(),
		DaggerVersion: runtimeVersion, UnifiedClient: true, GoImage: "golang:1.26-alpine",
	}
	require.NoError(t, Generate(t.Context(), cfg))
	bindings, err := os.ReadFile(filepath.Join(root, "internal/dagger/dagger.gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(bindings), "type SshfsVolumeOpts = core.SshfsVolumeOpts")
	self, err := os.ReadFile(filepath.Join(root, "internal/dagger/hello.gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(self), `Select("e2eTest")`)
}
