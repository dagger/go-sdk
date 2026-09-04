// Command codegen generates Go client code from a pre-computed introspection
// schema. A standalone client gets its own go.mod. Package mode writes into an
// existing Go module and updates the parent go.mod instead.
//
// It is intentionally engine-free: the schema and the bound module's metadata
// are supplied as files, so no nested engine session is opened.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"codegen/generator"
	gogenerator "codegen/generator/gogenerator"
	"codegen/introspection"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "codegen:", err)
		os.Exit(1)
	}
}

// clientMeta is the bound module's metadata the SDK reads off client.module /
// client.moduleSource and writes to --client-meta-path. It mirrors the subset
// the client generator needs (see generator.ClientGeneratorConfig).
type clientMeta struct {
	ModuleName    string                `json:"moduleName"`
	EngineVersion string                `json:"engineVersion"`
	Module        generator.BoundModule `json:"module"`
}

// validateBoundModuleKind fails closed on a source kind the generated client
// has no serve path for, rather than emit a client that silently mis-serves. A
// client serves the one module it binds to: GIT_SOURCE serves from a canonical
// ref+pin; LOCAL_SOURCE and DIR_SOURCE (how a workspace-local module resolves in
// practice) serve by resolving the workspace-relative path against the workspace.
func validateBoundModuleKind(m generator.BoundModule) error {
	switch m.Kind {
	case generator.ModuleKindGit, generator.ModuleKindLocal, generator.ModuleKindDir:
		return nil
	default:
		return fmt.Errorf("bound module has unsupported source kind %q", m.Kind)
	}
}

func run() error {
	var (
		introspectionPath = flag.String("introspection-json-path", "", "path to the introspection schema JSON")
		clientMetaPath    = flag.String("client-meta-path", "", "path to the client meta JSON (name, engineVersion, bound module)")
		outputDir         = flag.String("output", ".", "output directory for the generated client")
		moduleRoot        = flag.String("module-root", "", "root of the existing Go module that owns the generated package")
	)
	flag.Parse()

	if *introspectionPath == "" {
		return fmt.Errorf("--introspection-json-path is required")
	}

	introspectionJSON, err := os.ReadFile(*introspectionPath)
	if err != nil {
		return fmt.Errorf("read introspection json: %w", err)
	}
	var resp introspection.Response
	if err := json.Unmarshal(introspectionJSON, &resp); err != nil {
		return fmt.Errorf("unmarshal introspection json: %w", err)
	}
	if resp.Schema == nil {
		return fmt.Errorf("introspection json has no __schema")
	}

	clientConfig := &generator.ClientGeneratorConfig{}
	if *clientMetaPath != "" {
		metaJSON, err := os.ReadFile(*clientMetaPath)
		if err != nil {
			return fmt.Errorf("read client meta json: %w", err)
		}
		var meta clientMeta
		if err := json.Unmarshal(metaJSON, &meta); err != nil {
			return fmt.Errorf("unmarshal client meta json: %w", err)
		}
		clientConfig.ModuleName = meta.ModuleName
		clientConfig.EngineVersion = meta.EngineVersion
		clientConfig.BoundModule = meta.Module

		if err := validateBoundModuleKind(meta.Module); err != nil {
			return err
		}
	}

	cfg := generator.Config{
		OutputDir:    *outputDir,
		ClientConfig: clientConfig,
	}
	if *moduleRoot != "" {
		packageImport, err := packageImportPath(*moduleRoot, *outputDir)
		if err != nil {
			return err
		}
		cfg.PackageImport = packageImport
		if err := updateModuleGoMod(*moduleRoot, clientConfig.EngineVersion); err != nil {
			return err
		}
	}

	generator.SetSchemaParents(resp.Schema)

	gen := &gogenerator.GoGenerator{Config: cfg}

	ctx := context.Background()
	state, err := gen.GenerateClient(ctx, resp.Schema, resp.SchemaVersion)
	if err != nil {
		return fmt.Errorf("generate client: %w", err)
	}

	if err := generator.Overlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated client: %w", err)
	}

	return nil
}

func packageImportPath(moduleRoot, outputDir string) (string, error) {
	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		return "", fmt.Errorf("resolve module root: %w", err)
	}
	output, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	rel, err := filepath.Rel(root, output)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output directory %q is outside module root %q", outputDir, moduleRoot)
	}

	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read module go.mod: %w", err)
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", fmt.Errorf("parse module go.mod: %w", err)
	}
	if file.Module == nil || file.Module.Mod.Path == "" {
		return "", fmt.Errorf("module go.mod has no module path")
	}
	if rel == "." {
		return file.Module.Mod.Path, nil
	}
	return path.Join(file.Module.Mod.Path, filepath.ToSlash(rel)), nil
}

func updateModuleGoMod(moduleRoot, engineVersion string) error {
	if engineVersion == "" {
		return nil
	}
	goModPath := filepath.Join(moduleRoot, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("read module go.mod: %w", err)
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return fmt.Errorf("parse module go.mod: %w", err)
	}
	for _, replace := range file.Replace {
		if replace.Old.Path == "dagger.io/dagger" {
			return nil
		}
	}
	for _, require := range file.Require {
		if require.Mod.Path == "dagger.io/dagger" && semver.Compare(require.Mod.Version, engineVersion) >= 0 {
			return nil
		}
	}
	if err := file.AddRequire("dagger.io/dagger", engineVersion); err != nil {
		return fmt.Errorf("update dagger.io/dagger requirement: %w", err)
	}
	updated, err := file.Format()
	if err != nil {
		return fmt.Errorf("format module go.mod: %w", err)
	}
	if bytes.Equal(data, updated) {
		return nil
	}
	if err := os.WriteFile(goModPath, updated, 0o600); err != nil {
		return fmt.Errorf("write module go.mod: %w", err)
	}
	return nil
}
