package gogenerator

import (
	"context"
	"fmt"
	"io/fs"
	"path"

	"github.com/psanford/memfs"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
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
	if g.Config.UnifiedClient {
		cfg := g.Config
		cfg.PackageImport = packageImport
		cfg.PackageName = "dagger"
		cfg.ClientConfig = nil // The engine has already served the module schema.
		state, err := (&GoGenerator{Config: cfg}).GenerateUnifiedClient(ctx, schema, schemaVersion)
		if err != nil {
			return nil, err
		}
		mfs := memfs.New()
		if err := mfs.MkdirAll("internal/dagger", 0755); err != nil {
			return nil, err
		}
		if err := copyOverlay(state.Overlay, mfs); err != nil {
			return nil, err
		}
		return &generator.GeneratedState{Overlay: mfs}, nil
	}
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

func copyOverlay(source fs.FS, target *memfs.FS) error {
	return fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		dest := path.Join("internal/dagger", name)
		if err := target.MkdirAll(path.Dir(dest), 0755); err != nil {
			return err
		}
		return target.WriteFile(dest, data, 0644)
	})
}
