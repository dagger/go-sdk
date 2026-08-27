// Command codegen generates Go client and module code from a pre-computed
// introspection schema.
//
// It is intentionally engine-free: the schema (and, for a client, the bound
// module's metadata; for a module, its own already-merged types) is supplied
// as files, so no nested engine session is opened. A module's self-type merge
// is done by the caller (the SDK's Dang layer) through the engine's
// schema().merge(), between the two module modes.
//
// Modes:
//
//	codegen [generate-client] [--introspection-json-path P] \
//	    [--client-meta-path P] [--output D]
//	    generate a standalone Go client (the default when no subcommand
//	    is given)
//	codegen module-types --introspection-json-path P --output D \
//	    --module-root-path R --module-source-path S --module-parent-path P \
//	    --module-name N --lib-version V --module-types-out P
//	    bootstrap the module and emit its own types as introspection JSON
//	    for the caller to merge into the dependency schema
//	codegen generate-module --introspection-json-path P --output D \
//	    --module-root-path R --module-source-path S --module-parent-path P \
//	    --module-name N --lib-version V --stale-out P --gowork-out P
//	    generate the module's bindings from the merged schema
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codegen/generator"
	gogenerator "codegen/generator/gogenerator"
	"codegen/introspection"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "codegen:", err)
		os.Exit(1)
	}
}

// run dispatches on the optional leading subcommand. A first argument that
// does not start with "-" selects a mode; otherwise (flags start immediately)
// it is the client mode, preserving the `codegen --introspection-...`
// invocation the SDK already uses.
func run() error {
	ctx := context.Background()
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "generate-client":
			return runGenerateClient(ctx, args[1:])
		case "module-types":
			return runModuleTypes(ctx, args[1:])
		case "generate-module":
			return runGenerateModule(ctx, args[1:])
		default:
			return fmt.Errorf("unknown command %q", args[0])
		}
	}
	return runGenerateClient(ctx, args)
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

func runGenerateClient(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("generate-client", flag.ContinueOnError)
	introspectionPath := fs.String("introspection-json-path", "", "path to the introspection schema JSON")
	clientMetaPath := fs.String("client-meta-path", "", "path to the client meta JSON (name, engineVersion, bound module)")
	outputDir := fs.String("output", ".", "output directory for the generated client")
	if err := fs.Parse(args); err != nil {
		return err
	}

	schema, schemaVersion, err := readIntrospection(*introspectionPath)
	if err != nil {
		return err
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
	generator.SetSchemaParents(schema)

	gen := &gogenerator.GoGenerator{Config: cfg}
	state, err := gen.GenerateClient(ctx, schema, schemaVersion)
	if err != nil {
		return fmt.Errorf("generate client: %w", err)
	}
	if err := generator.Overlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated client: %w", err)
	}
	return nil
}

// moduleFlags are the flags common to module-types and generate-module.
type moduleFlags struct {
	introspectionPath string
	outputDir         string
	rootPath          string
	sourcePath        string
	parentPath        string
	name              string
	libVersion        string
}

func (mf *moduleFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&mf.introspectionPath, "introspection-json-path", "", "path to the introspection schema JSON")
	fs.StringVar(&mf.outputDir, "output", ".", "output directory (the module's context root)")
	fs.StringVar(&mf.rootPath, "module-root-path", "", "output-relative path to the module root, the directory holding dagger-module.toml")
	fs.StringVar(&mf.sourcePath, "module-source-path", "", "output-relative path to the module source")
	fs.StringVar(&mf.parentPath, "module-parent-path", "", "module-source-relative path back to the context root")
	fs.StringVar(&mf.name, "module-name", "", "name of the module to generate code for")
	fs.StringVar(&mf.libVersion, "lib-version", "", "dagger.io/dagger version to pin in the generated go.mod")
}

func (mf *moduleFlags) config() (generator.Config, error) {
	if mf.name == "" {
		return generator.Config{}, fmt.Errorf("--module-name is required")
	}
	if mf.rootPath == "" {
		return generator.Config{}, fmt.Errorf("--module-root-path is required")
	}
	if mf.sourcePath == "" {
		return generator.Config{}, fmt.Errorf("--module-source-path is required")
	}
	if mf.libVersion == "" {
		return generator.Config{}, fmt.Errorf("--lib-version is required")
	}
	return generator.Config{
		OutputDir: mf.outputDir,
		ModuleConfig: &generator.ModuleGeneratorConfig{
			ModuleName:       mf.name,
			ModuleRootPath:   mf.rootPath,
			ModuleSourcePath: mf.sourcePath,
			ModuleParentPath: mf.parentPath,
			LibVersion:       mf.libVersion,
		},
	}, nil
}

func runModuleTypes(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("module-types", flag.ContinueOnError)
	var mf moduleFlags
	mf.bind(fs)
	typesOut := fs.String("module-types-out", "", "path to write the module's types as introspection JSON (empty when the schema view predates self types)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *typesOut == "" {
		return fmt.Errorf("--module-types-out is required")
	}
	cfg, err := mf.config()
	if err != nil {
		return err
	}
	schema, schemaVersion, err := readIntrospection(mf.introspectionPath)
	if err != nil {
		return err
	}
	generator.SetSchemaParents(schema)

	gen := &gogenerator.GoGenerator{Config: cfg}
	for ctx.Err() == nil {
		state, typesJSON, err := gen.GenerateModuleTypes(ctx, schema, schemaVersion)
		if err != nil {
			return fmt.Errorf("emit module types: %w", err)
		}
		if err := applyState(ctx, cfg, state); err != nil {
			return err
		}
		if !state.NeedRegenerate {
			return os.WriteFile(*typesOut, typesJSON, 0o644)
		}
	}
	return ctx.Err()
}

func runGenerateModule(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("generate-module", flag.ContinueOnError)
	var mf moduleFlags
	mf.bind(fs)
	staleOut := fs.String("stale-out", "", "path to write the output-relative paths of stale dependency bindings, one per line")
	goworkOut := fs.String("gowork-out", "", "path to write the output-relative path of the go.work in effect, empty when none")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := mf.config()
	if err != nil {
		return err
	}
	schema, schemaVersion, err := readIntrospection(mf.introspectionPath)
	if err != nil {
		return err
	}
	generator.SetSchemaParents(schema)

	gen := &gogenerator.GoGenerator{Config: cfg}
	for ctx.Err() == nil {
		state, err := gen.GenerateModule(ctx, schema, schemaVersion)
		if err != nil {
			return fmt.Errorf("generate module: %w", err)
		}
		if err := applyState(ctx, cfg, state); err != nil {
			return err
		}
		if state.NeedRegenerate {
			continue
		}
		if *staleOut != "" {
			if err := writeLines(*staleOut, state.RemovePaths); err != nil {
				return err
			}
		}
		if *goworkOut != "" {
			gowork, err := gogenerator.GoWorkRelPath(cfg.OutputDir, cfg.ModuleConfig.ModuleSourcePath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(*goworkOut, []byte(gowork), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	return ctx.Err()
}

func writeLines(path string, lines []string) error {
	var out strings.Builder
	for _, line := range lines {
		out.WriteString(filepath.ToSlash(line))
		out.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

// applyState removes stale paths, writes the generated overlay onto the
// output dir and runs the state's post-commands in the module source
// directory, mirroring cmd/codegen's Generate loop. A failing post-command is
// fatal, as it is under the engine: tolerating it would leave go.mod half
// rewritten.
func applyState(ctx context.Context, cfg generator.Config, state *generator.GeneratedState) error {
	if err := generator.Apply(ctx, state, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated code: %w", err)
	}
	for _, cmd := range state.PostCommands {
		if cmd.Dir == "" {
			cmd.Dir = filepath.Join(cfg.OutputDir, cfg.ModuleConfig.ModuleSourcePath)
		}
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		fmt.Fprintln(os.Stderr, "running:", strings.Join(cmd.Args, " "))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("post-command %q: %w", strings.Join(cmd.Args, " "), err)
		}
	}
	return nil
}

func readIntrospection(path string) (*introspection.Schema, string, error) {
	if path == "" {
		return nil, "", fmt.Errorf("--introspection-json-path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read introspection json: %w", err)
	}
	var resp introspection.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, "", fmt.Errorf("unmarshal introspection json: %w", err)
	}
	if resp.Schema == nil {
		return nil, "", fmt.Errorf("introspection json has no __schema")
	}
	return resp.Schema, resp.SchemaVersion, nil
}
