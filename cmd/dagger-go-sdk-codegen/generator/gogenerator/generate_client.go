package gogenerator

import (
	"context"
	"fmt"

	"github.com/psanford/memfs"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// GenerateClient generates a Go client package for the given schema:
// dagger.gen.go with the core bindings, one <module>.gen.go for the bound
// module, and a dag/ convenience package.
func (g *GoGenerator) GenerateClient(ctx context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	if g.Config.PackageName == "" {
		cfg := g.Config
		cfg.PackageName = clientPackageName(cfg.OutputDir, cfg.PackageImport)
		g = &GoGenerator{Config: cfg}
	}
	if g.Config.UnifiedClient {
		return g.GenerateUnifiedClient(ctx, schema, schemaVersion)
	}
	generator.SetSchema(schema)

	mfs := memfs.New()
	if g.Config.PackageImport == "" {
		return nil, fmt.Errorf("package import path is required")
	}

	if err := generateCode(ctx, g.Config, schema, schemaVersion, mfs, &PackageInfo{
		PackageName:   g.Config.PackageName,
		PackageImport: g.Config.PackageImport,
	}); err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	return &generator.GeneratedState{
		Overlay: mfs,
	}, nil
}
