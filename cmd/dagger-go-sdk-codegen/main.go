// Command dagger-go-sdk-codegen generates Go bindings for the Dagger API.
//
// Usage:
//
//	dagger-go-sdk-codegen client [flags]
//
// The client subcommand generates a standalone client package for one module
// from a pre-computed introspection schema. It writes into an existing Go
// module and updates that module's go.mod. It is engine-free: the schema and
// the bound module's metadata are supplied as files, so no nested engine
// session is opened.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	gogenerator "github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator/gogenerator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

const usage = `Usage: dagger-go-sdk-codegen <command> [flags]

Commands:
  client  Generate a standalone client package for one module

Run "dagger-go-sdk-codegen <command> -h" for the flags of a command.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dagger-go-sdk-codegen:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("missing command")
	}
	switch args[0] {
	case "client":
		return runClient(args[1:])
	case "-h", "-help", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// clientMeta is the bound module's metadata the SDK reads off client.module /
// client.moduleSource and writes to --client-meta-path. It mirrors the subset
// the client generator needs (see generator.ClientGeneratorConfig).
type clientMeta struct {
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

func runClient(args []string) error {
	flags := flag.NewFlagSet("client", flag.ContinueOnError)
	var (
		introspectionPath = flags.String("introspection-json-path", "", "path to the introspection schema JSON")
		clientMetaPath    = flags.String("client-meta-path", "", "path to the client meta JSON (engine version and bound module)")
		outputDir         = flags.String("output", ".", "output directory for the generated client")
		moduleRoot        = flags.String("module-root", "", "root of the Go module that owns the generated package")
	)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

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
	if err := updateModuleGoMod(*moduleRoot, meta.EngineVersion); err != nil {
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
	// An engine release does not imply a published dagger.io/dagger of the same
	// version: a development engine reports the next, unreleased version. Pinning
	// that leaves a requirement nothing can resolve, and every later go command
	// in the module fails on it — including the `go get dagger.io/dagger@<commit>`
	// the engine's own module codegen runs, which is what would have supplied a
	// usable version.
	if !moduleVersionExists(moduleRoot, "dagger.io/dagger", engineVersion) {
		return nil
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
