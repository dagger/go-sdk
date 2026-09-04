package gogenerator

import (
	"context"
	"fmt"

	"github.com/psanford/memfs"

	"codegen/generator"
	"codegen/introspection"
)

// GenerateClient generates a Go client package for the given schema:
// dagger.gen.go with the core bindings, one <module>.gen.go for the bound
// module, and a dag/ convenience package.
func (g *GoGenerator) GenerateClient(ctx context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	generator.SetSchema(schema)

	mfs := memfs.New()
	if g.Config.PackageImport == "" {
		return nil, fmt.Errorf("package import path is required")
	}

	if err := generateCode(ctx, g.Config, schema, schemaVersion, mfs, &PackageInfo{
		PackageName:   "dagger",
		PackageImport: g.Config.PackageImport,
	}); err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	return &generator.GeneratedState{
		Overlay: mfs,
	}, nil
}
