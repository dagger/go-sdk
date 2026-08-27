package generator

import (
	"context"
	"io/fs"
	"os/exec"

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

	// RemovePaths are paths, relative to the output directory, that should be
	// removed before applying Overlay. Generators use this to reconcile files
	// they emitted previously but no longer emit.
	RemovePaths []string

	// PostCommands are commands that need to be run after the codegen has
	// finished. This is used for example to run `go mod tidy` after generating
	// Go code.
	PostCommands []*exec.Cmd

	// NeedRegenerate indicates that the code needs to be generated again. This
	// can happen if the codegen spat out templates that depend on generated
	// types. In that case the codegen needs to be run again with both the
	// templates and the initially generated types available.
	NeedRegenerate bool
}
