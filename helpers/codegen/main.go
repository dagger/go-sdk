// Command codegen generates a Go client package from a pre-computed
// introspection schema. It writes into an existing Go module and updates that
// module's go.mod.
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
	"os/exec"
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

// clientMeta combines the bound module's schema compatibility metadata with
// the Go client runtime version selected by this SDK.
type clientMeta struct {
	EngineVersion string                `json:"engineVersion"`
	ClientVersion string                `json:"clientVersion"`
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
		clientMetaPath    = flag.String("client-meta-path", "", "path to the client meta JSON (engine version and bound module)")
		outputDir         = flag.String("output", ".", "output directory for the generated client")
		moduleRoot        = flag.String("module-root", "", "root of the Go module that owns the generated package")
	)
	flag.Parse()

	if *introspectionPath == "" {
		return fmt.Errorf("--introspection-json-path is required")
	}
	if *clientMetaPath == "" {
		return fmt.Errorf("--client-meta-path is required")
	}
	if *moduleRoot == "" {
		return fmt.Errorf("--module-root is required")
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

	metaJSON, err := os.ReadFile(*clientMetaPath)
	if err != nil {
		return fmt.Errorf("read client meta json: %w", err)
	}
	var meta clientMeta
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return fmt.Errorf("unmarshal client meta json: %w", err)
	}
	if err := validateBoundModuleKind(meta.Module); err != nil {
		return err
	}

	packageImport, err := packageImportPath(*moduleRoot, *outputDir)
	if err != nil {
		return err
	}
	cfg := generator.Config{
		OutputDir:     *outputDir,
		PackageImport: packageImport,
		ClientConfig:  &generator.ClientGeneratorConfig{BoundModule: meta.Module},
	}
	if err := updateModuleGoMod(*moduleRoot, meta.ClientVersion); err != nil {
		return err
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

// moduleVersionExists reports whether the module proxy can resolve the given
// version. Resolution needs the network, so a failure to reach it reads as
// "cannot pin" rather than as an error: skipping the pin leaves a module the
// toolchain can still resolve on its own.
func moduleVersionExists(dir, modulePath, version string) bool {
	cmd := exec.Command("go", "list", "-m", "-e", "-f", "{{if .Error}}unresolved{{end}}", modulePath+"@"+version)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == ""
}

func updateModuleGoMod(moduleRoot, clientVersion string) error {
	if clientVersion == "" {
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
		if require.Mod.Path == "dagger.io/dagger" && semver.Compare(require.Mod.Version, clientVersion) >= 0 {
			return nil
		}
	}
	// A configured client version may be a development version without a
	// published dagger.io/dagger counterpart. Do not leave the consumer module
	// with an unresolvable requirement.
	if !moduleVersionExists(moduleRoot, "dagger.io/dagger", clientVersion) {
		return nil
	}
	if err := file.AddRequire("dagger.io/dagger", clientVersion); err != nil {
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
