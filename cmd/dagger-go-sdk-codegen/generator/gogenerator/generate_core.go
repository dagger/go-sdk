package gogenerator

import (
	"context"
	"fmt"

	"github.com/psanford/memfs"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

const (
	// CoreGenFile is the path of the generated dagger.io/dagger/core bindings,
	// relative to the root of the dagger.io/dagger module.
	CoreGenFile = "core/core.gen.go"

	// DagGenFile is the path of the generated dagger.io/dagger/dag package,
	// relative to the root of the dagger.io/dagger module.
	DagGenFile = "dag/dag.gen.go"
)

// GenerateCore generates the core bindings of dagger.io/dagger: CoreGenFile
// and DagGenFile. The schema must contain the core API only.
func (g *GoGenerator) GenerateCore(ctx context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	if !g.Config.CoreLibrary {
		return nil, fmt.Errorf("core generation requires Config.CoreLibrary")
	}
	if deps := schema.DependencyNames(); len(deps) > 0 {
		return nil, fmt.Errorf("schema contains module types from %v: core bindings need the core API only", deps)
	}
	generator.SetSchema(schema)

	mfs := memfs.New()
	if err := generateCode(ctx, g.Config, schema, schemaVersion, mfs, &PackageInfo{
		PackageName:   "core",
		PackageImport: "dagger.io/dagger/core",
	}); err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	return &generator.GeneratedState{
		Overlay: mfs,
	}, nil
}
