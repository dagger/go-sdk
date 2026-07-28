package generator

import (
	"context"
	"io/fs"

	"codegen/introspection"
)

type Generator interface {
	// GenerateClient runs codegen in a context of a standalone client and
	// returns an overlay filesystem with the generated files.
	GenerateClient(ctx context.Context, schema *introspection.Schema, schemaVersion string) (*GeneratedState, error)
}

type GeneratedState struct {
	// Overlay is the overlay filesystem that contains generated code to write
	// over the output directory.
	Overlay fs.FS
}
