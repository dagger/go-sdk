package gogenerator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// checksSchema is the client schema of a module with a check, as the
// engine renders it in the target's view: before v1 the check keeps its Void
// type and no Check type exists; on v1 a check is Check!.
func checksSchema(v1 bool) *introspection.Schema {
	schema := buildClientSchema()
	void := &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "Void"}
	hello := schema.Types[1]
	hello.Fields = append(hello.Fields,
		&introspection.Field{Name: "test", TypeRef: void, Directives: introspection.Directives{{Name: "check"}, newSourceMapDirective("hello")}},
		&introspection.Field{Name: "build", TypeRef: void, Directives: introspection.Directives{newSourceMapDirective("hello")}},
	)
	if v1 {
		check := &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: "Check"}}
		hello.Fields = append(hello.Fields, &introspection.Field{Name: "lint", TypeRef: check, Directives: introspection.Directives{{Name: "check"}, newSourceMapDirective("hello")}})
		schema.Types = append(schema.Types, &introspection.Type{Kind: introspection.TypeKindObject, Name: "Check"})
	}
	return schema
}

var helloMethod = regexp.MustCompile(`(?s)func \(r \*Hello\) (Build|Lint|Test)\(.*?\n}\n`)

// A unified client runs in a v1 session, where every check is Check!, even
// when the target's schema predates v1. Other functions keep their type.
func TestUnifiedClientProjectsChecks(t *testing.T) {
	for name, v1 := range map[string]bool{"target-before-v1": false, "target-on-v1": true} {
		t.Run(name, func(t *testing.T) {
			gen := &GoGenerator{Config: generator.Config{UnifiedClient: true, OutputDir: t.TempDir(), PackageImport: "example.com/app/client", ClientConfig: &generator.ClientGeneratorConfig{BoundModule: generator.BoundModule{Kind: generator.ModuleKindDir, Path: "hello"}}}}
			schema := checksSchema(v1)
			state, err := gen.GenerateClient(t.Context(), schema, "v0.20.6")
			require.NoError(t, err)
			got := strings.Join(helloMethod.FindAllString(readOverlay(t, state, "hello.gen.go"), -1), "\n")
			want, err := os.ReadFile(filepath.Join("testdata", "checks", name+".golden"))
			require.NoError(t, err)
			require.Equal(t, string(want), got)
			require.Contains(t, readOverlay(t, state, "dagger.gen.go"), "type Check = core.Check\n")
			require.Equal(t, "Void", schema.Types[1].Fields[1].TypeRef.Name, "the caller's schema changed")
		})
	}
}

// Embedded bindings carry the target's own core, so a check keeps the type
// of the target's view.
func TestEmbeddedClientKeepsCheckType(t *testing.T) {
	schema := checksSchema(false)
	generator.SetSchemaParents(schema)
	bindings := generateUnifiedHello(t, schema, "v0.20.6", false)
	require.Contains(t, bindings, "func (r *Hello) Test(ctx context.Context) error")
}
