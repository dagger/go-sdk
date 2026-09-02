package gogenerator

import (
	"context"
	"fmt"

	"github.com/psanford/memfs"

	"codegen/generator"
	"codegen/introspection"
)

// GenerateEmbeddedClient generates the client package used by a Go module.
// It uses the same renderer as standalone clients. It writes the package below
// internal/dagger and does not create a nested go.mod.
func (g *GoGenerator) GenerateEmbeddedClient(
	ctx context.Context,
	schema *introspection.Schema,
	schemaVersion string,
	packageImport string,
) (*generator.GeneratedState, error) {
	generator.SetSchema(schema)

	mfs := memfs.New()
	if err := mfs.MkdirAll("internal/dagger", 0o755); err != nil {
		return nil, fmt.Errorf("create embedded client directory: %w", err)
	}
	clientFS, err := mfs.Sub("internal/dagger")
	if err != nil {
		return nil, fmt.Errorf("open embedded client directory: %w", err)
	}

	cfg := g.Config
	// The embedded package uses the established module client bootstrap. It
	// attaches to the nested session through DAGGER_SESSION_* and does not
	// expose the standalone client's Connect(ctx) surface.
	cfg.ClientConfig = nil
	if err := generateCode(ctx, cfg, schema, schemaVersion, clientFS.(*memfs.FS), &PackageInfo{
		PackageName:   "dagger",
		PackageImport: packageImport,
	}); err != nil {
		return nil, fmt.Errorf("generate embedded client: %w", err)
	}

	return &generator.GeneratedState{Overlay: mfs}, nil
}
