package gogenerator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/psanford/memfs"
	"golang.org/x/mod/modfile"

	"codegen/generator"
	"codegen/introspection"
)

// GenerateClient generates a standalone Go client for the given (client)
// schema: dagger.gen.go with the core bindings, one <module>.gen.go for the
// bound module, a dag/ convenience package and a go.mod pinning
// dagger.io/dagger at the engine version.
//
// The go.mod handling is deliberately offline-friendly: a fresh client dir
// gets a deterministic go.mod (no `go mod tidy`, no parent go.mod editing);
// an existing go.mod is preserved verbatim except for bumping the
// dagger.io/dagger requirement (unless a custom replace directive pins it).
func (g *GoGenerator) GenerateClient(ctx context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	generator.SetSchema(schema)

	mfs := memfs.New()

	// Read the client dir's go.mod, if any.
	goModPath := filepath.Join(g.Config.OutputDir, "go.mod")
	existingGoModData, readErr := os.ReadFile(goModPath)
	isInstall := errors.Is(readErr, os.ErrNotExist)
	if readErr != nil && !isInstall {
		return nil, fmt.Errorf("failed to read go.mod: %w", readErr)
	}

	var existingGoMod *modfile.File
	if !isInstall {
		var err error
		existingGoMod, err = modfile.Parse("go.mod", existingGoModData, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to parse client go.mod: %w", err)
		}
	}

	// The generated client is a library package named "dagger". Its import
	// path is the client module path: an existing go.mod wins, otherwise the
	// bound module's name (lowercased) names the fresh module.
	packageImport := strings.ToLower(g.Config.ClientConfig.ModuleName)
	if existingGoMod != nil && existingGoMod.Module != nil {
		packageImport = existingGoMod.Module.Mod.Path
	}
	if packageImport == "" {
		return nil, fmt.Errorf("cannot name the client module: no module name in client metadata and no go.mod in %q", g.Config.OutputDir)
	}

	if err := generateCode(ctx, g.Config, schema, schemaVersion, mfs, &PackageInfo{
		PackageName:   "dagger",
		PackageImport: packageImport,
	}); err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	var goModBody []byte
	var err error
	if isInstall {
		goModBody, err = g.freshClientGoMod(packageImport)
	} else {
		goModBody, err = g.updatedClientGoMod(existingGoMod)
	}
	if err != nil {
		return nil, err
	}

	if err := mfs.WriteFile("go.mod", goModBody, 0600); err != nil {
		return nil, fmt.Errorf("failed to write client go.mod: %w", err)
	}

	return &generator.GeneratedState{
		Overlay: mfs,
	}, nil
}

// freshClientGoMod builds the go.mod for a brand new client dir: module path,
// go version and a dagger.io/dagger requirement pinned at the engine version.
func (g *GoGenerator) freshClientGoMod(modulePath string) ([]byte, error) {
	clientGoMod := new(modfile.File)
	if err := clientGoMod.AddModuleStmt(modulePath); err != nil {
		return nil, fmt.Errorf("failed to add module statement to client go.mod: %w", err)
	}
	if err := clientGoMod.AddGoStmt(goVersion); err != nil {
		return nil, fmt.Errorf("failed to add go statement to client go.mod: %w", err)
	}

	// Pin dagger.io/dagger to the engineVersion from dagger.json (replace
	// directives added by tests/users will override this).
	if engineVersion := g.Config.ClientConfig.EngineVersion; engineVersion != "" {
		if err := clientGoMod.AddRequire(daggerImportPath, engineVersion); err != nil {
			return nil, fmt.Errorf("failed to require %s in client go.mod: %w", daggerImportPath, err)
		}
	}

	body, err := clientGoMod.Format()
	if err != nil {
		return nil, fmt.Errorf("failed to format client go.mod: %w", err)
	}
	return body, nil
}

// updatedClientGoMod preserves an existing go.mod verbatim (module name,
// requires, local replace directives) and only bumps the dagger.io/dagger
// requirement — unless the user pinned it with a custom replace directive.
func (g *GoGenerator) updatedClientGoMod(existingGoMod *modfile.File) ([]byte, error) {
	engineVersion := g.Config.ClientConfig.EngineVersion
	if engineVersion != "" && !isDaggerPkgCustomReplaced(existingGoMod.Replace) {
		if err := existingGoMod.AddRequire(daggerImportPath, engineVersion); err != nil {
			return nil, fmt.Errorf("failed to require %s in client go.mod: %w", daggerImportPath, err)
		}
	}

	body, err := existingGoMod.Format()
	if err != nil {
		return nil, fmt.Errorf("failed to format client go.mod: %w", err)
	}
	return body, nil
}
