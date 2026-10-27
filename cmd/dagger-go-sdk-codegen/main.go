// Command dagger-go-sdk-codegen generates Go bindings for the Dagger API.
//
// Usage:
//
//	dagger-go-sdk-codegen client [flags]
//	dagger-go-sdk-codegen core [flags]
//
//	dagger-go-sdk-codegen normalize-source-links [flags]
//
// The core subcommand generates the core bindings of dagger.io/dagger: the
// dagger.io/dagger/core package, and optionally the deprecated
// dagger.io/dagger/dag package. By default it connects to the Dagger engine
// with dagger.Connect and reads the live schema.
//
// The client subcommand generates a standalone client package for one module
// from a pre-computed introspection schema. It writes into an existing Go
// module and updates that module's go.mod. It is engine-free: the schema and
// the bound module's metadata are supplied as files, so no nested engine
// session is opened.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"dagger.io/dagger"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	gogenerator "github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator/gogenerator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/library"
	"github.com/psanford/memfs"
	"golang.org/x/mod/modfile"
)

const usage = `Usage: dagger-go-sdk-codegen <command> [flags]

Commands:
  client  Generate a standalone client package for one module
  check-library  Check the dagger.io/dagger a Go module selects against the targeted release
  core    Generate the core bindings of dagger.io/dagger
  normalize-source-links  Restore local source positions in workspace module bindings

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
	case "check-library":
		return runCheckLibrary(args[1:])
	case "core":
		return runCore(args[1:])
	case "normalize-source-links":
		return runNormalizeSourceLinks(args[1:])
	case "prune-clients":
		return runPruneClients(args[1:])
	case "go-version":
		flags := flag.NewFlagSet("go-version", flag.ContinueOnError)
		path := flags.String("go-mod", "go.mod", "consumer go.mod")
		minimum := flags.String("minimum", "1.26", "minimum Go language version")
		if err := flags.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		value, err := requiredGoVersion(*path, *minimum)
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil
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
	// TargetVersion is the dagger.io/dagger release the client pins: the
	// targeted release of the module that generates it, or the SDK's choice.
	TargetVersion string `json:"targetVersion"`
	// Subject names the module or scope the client is generated for, in
	// messages about its library.
	Subject string `json:"subject"`
	// Workspace and Git are set for a GIT_SOURCE module, to recognize a module
	// of the Git workspace that generates the client.
	Workspace workspaceMeta `json:"workspace"`
	Git       gitMeta       `json:"git"`
}

type workspaceMeta struct {
	Address string `json:"address"`
	Commit  string `json:"commit"`
}

type gitMeta struct {
	CloneRef    string `json:"cloneRef"`
	HTMLRepoURL string `json:"htmlRepoURL"`
}

// workspaceModule reports whether a Git module is part of the Git workspace
// that generates its client: in its repository, at its commit and selector.
// Its address then names that commit, which every commit changes. A workspace
// without a selector resolves its modules at the default branch, which its
// address does not name, so any selector at its commit matches.
func workspaceModule(meta clientMeta) bool {
	if meta.Module.Kind != generator.ModuleKindGit || meta.Git.CloneRef == "" || meta.Module.Pin == "" || meta.Module.Pin != meta.Workspace.Commit {
		return false
	}
	workspaceSelector, ok := gitSelector(meta.Workspace.Address, meta.Git.CloneRef)
	if !ok {
		return false
	}
	moduleSelector, ok := gitSelector(meta.Module.Ref, meta.Git.CloneRef)
	return ok && (workspaceSelector == "" || workspaceSelector == moduleSelector)
}

// gitSelector returns the version of a ref of the repository at cloneRef, or
// false for a ref of another repository. The ref is either in the legacy form
// (repo/subpath@version) or, since dagger v1.0.0-beta.20, in the explicit Git
// URL form (repo#version:subpath).
func gitSelector(ref, cloneRef string) (string, bool) {
	rest, ok := strings.CutPrefix(trimURLScheme(ref), trimURLScheme(cloneRef))
	if !ok || (rest != "" && rest[0] != '/' && rest[0] != '@' && rest[0] != '#') {
		return "", false
	}
	if version, explicit := strings.CutPrefix(rest, "#"); explicit {
		version, _, _ = strings.Cut(version, ":")
		return version, true
	}
	_, selector, _ := strings.Cut(rest, "@")
	return selector, true
}

// trimURLScheme drops a URL scheme: a GitHub workspace address has none,
// while the clone URL of its modules has https://.
func trimURLScheme(ref string) string {
	if _, rest, ok := strings.Cut(ref, "://"); ok {
		return rest
	}
	return ref
}

// localSourceLinks rewrites the links into the workspace commit to the local
// source positions a local workspace generates, so that the client does not
// change with every commit. Client bindings render those relative to "../..",
// as moduleRelPath does without a module.
func localSourceLinks(overlay fs.FS, schema *introspection.Schema, schemaVersion, repo, pin string) (fs.FS, error) {
	anchor, err := workspaceSourceLinkAnchor(repo, pin)
	if err != nil {
		return nil, err
	}
	catalog := newSourceLinkCatalog(schema, schemaVersion)
	normalized := memfs.New()
	err = fs.WalkDir(overlay, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == "." {
			return err
		}
		if entry.IsDir() {
			return normalized.MkdirAll(name, 0o755)
		}
		data, err := fs.ReadFile(overlay, name)
		if err != nil {
			return err
		}
		if strings.HasSuffix(name, ".go") {
			if data, err = normalizeGeneratedSourceLinks(name, data, catalog, anchor, "../.."); err != nil {
				return err
			}
		}
		return normalized.WriteFile(name, data, 0o644)
	})
	return normalized, err
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
		unified           = flags.Bool("unified", false, "reuse dagger.io/dagger/core types and a shared session")
		removedPath       = flags.String("removed-list", "", "append removed generated paths relative to module-root")
	)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
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
	pin := meta.Module.Pin
	inWorkspace := workspaceModule(meta)
	if inWorkspace {
		meta.Module = generator.BoundModule{Kind: generator.ModuleKindLocal, Path: meta.Module.Path}
	}

	packageImport, err := packageImportPath(*moduleRoot, *outputDir)
	if err != nil {
		return err
	}
	cfg := generator.Config{
		OutputDir:     *outputDir,
		PackageImport: packageImport,
		ClientConfig:  &generator.ClientGeneratorConfig{BoundModule: meta.Module, Version: meta.TargetVersion},
		UnifiedClient: *unified,
	}

	generator.SetSchemaParents(resp.Schema)

	gen := &gogenerator.GoGenerator{Config: cfg}

	ctx := context.Background()
	state, err := gen.GenerateClient(ctx, resp.Schema, resp.SchemaVersion)
	if err != nil {
		return fmt.Errorf("generate client: %w", err)
	}
	if inWorkspace {
		if state.Overlay, err = localSourceLinks(state.Overlay, resp.Schema, resp.SchemaVersion, meta.Git.HTMLRepoURL, pin); err != nil {
			return fmt.Errorf("normalize client source links: %w", err)
		}
	}

	if err := generator.ValidateOwnedOverlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return err
	}
	clientVersion, explicit := meta.TargetVersion, meta.TargetVersion != ""
	if !explicit {
		if *unified {
			data, err := os.ReadFile(filepath.Join(*moduleRoot, "go.mod"))
			if err != nil {
				return err
			}
			mod, err := modfile.Parse("go.mod", data, nil)
			if err != nil {
				return err
			}
			replaced := false
			for _, replacement := range mod.Replace {
				if replacement.Old.Path == "dagger.io/dagger" {
					replaced = true
				}
			}
			if !replaced {
				return fmt.Errorf("unified clients require targetVersion: use a published SDK containing dagger.io/dagger/core and ModuleGraphQLClient, or a local SDK replacement")
			}
		}
		clientVersion = meta.EngineVersion // Compatibility with older SDK metadata.
	}
	goModPath := filepath.Join(*moduleRoot, "go.mod")
	if explicit {
		replaced, err := library.Replaced(goModPath, clientVersion)
		if err != nil {
			return err
		}
		if !replaced {
			if err := library.CheckResolvable(*moduleRoot, subject(meta), clientVersion); err != nil {
				return err
			}
		}
	}
	// An engine version is pinned only when it is published: a development
	// engine reports the next, unreleased version, and a requirement nothing
	// can resolve fails every later go command in the module.
	if err := library.Pin(goModPath, clientVersion, explicit); err != nil {
		return fmt.Errorf("update %s requirement: %w", library.Path, err)
	}
	removed, err := generator.WriteOwnedOverlay(ctx, state.Overlay, cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("write generated client: %w", err)
	}
	if *removedPath != "" {
		prefix, err := filepath.Rel(*moduleRoot, cfg.OutputDir)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(*removedPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		for _, p := range removed {
			if _, err := fmt.Fprintln(f, filepath.ToSlash(filepath.Join(prefix, p))); err != nil {
				f.Close()
				return err
			}
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

const coreUsage = `Usage: dagger-go-sdk-codegen core [flags]

Generate the core bindings of dagger.io/dagger: the Go types for the core
Dagger API, in the dagger.io/dagger/core package.

The command connects to the Dagger engine with dagger.Connect and reads the
live API schema. To select an engine, set DAGGER_SESSION_PORT and
DAGGER_SESSION_TOKEN (for example, run the command inside "dagger run" or in
a container that has a Dagger session). If they are not set, dagger.Connect
starts an engine.

--output must be the dagger.io/dagger/core package directory, and
--dag-output the dagger.io/dagger/dag package directory. The command finds
the go.mod of the dagger.io/dagger module from these directories.

Flags:
`

func runCore(args []string) error {
	flags := flag.NewFlagSet("core", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	flags.Usage = func() {
		fmt.Fprint(flags.Output(), coreUsage)
		flags.PrintDefaults()
	}
	var (
		outputDir         = flags.String("output", ".", "directory of the dagger.io/dagger/core package")
		dagOutputDir      = flags.String("dag-output", "", "directory of the deprecated dagger.io/dagger/dag package (optional; if empty, do not generate it)")
		introspectionPath = flags.String("introspection-json-path", "", "optional: read the schema from this introspection JSON file, not from the engine.\nThis is an escape hatch for advanced use. Most users do not need it.")
	)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	if err := checkPackageDir(*outputDir, "dagger.io/dagger/core"); err != nil {
		return fmt.Errorf("--output: %w", err)
	}
	if *dagOutputDir != "" {
		if err := checkPackageDir(*dagOutputDir, "dagger.io/dagger/dag"); err != nil {
			return fmt.Errorf("--dag-output: %w", err)
		}
	}

	ctx := context.Background()
	schema, schemaVersion, err := loadSchema(ctx, *introspectionPath)
	if err != nil {
		return err
	}
	generator.SetSchemaParents(schema)

	gen := &gogenerator.GoGenerator{Config: generator.Config{
		OutputDir:     *outputDir,
		PackageImport: "dagger.io/dagger/core",
		CoreLibrary:   true,
	}}
	state, err := gen.GenerateCore(ctx, schema, schemaVersion)
	if err != nil {
		return fmt.Errorf("generate core bindings: %w", err)
	}

	outputs := map[string]string{gogenerator.CoreGenFile: *outputDir, gogenerator.CoreCodecsGenFile: *outputDir}
	if *dagOutputDir != "" {
		outputs[gogenerator.DagGenFile] = *dagOutputDir
	}
	for src, dir := range outputs {
		data, err := fs.ReadFile(state.Overlay, src)
		if err != nil {
			return fmt.Errorf("read generated %s: %w", src, err)
		}
		dest := filepath.Join(dir, path.Base(src))
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
	}
	return nil
}

// loadSchema reads the API schema from an introspection JSON file if path is
// set. Otherwise it connects to the Dagger engine and introspects it.
func loadSchema(ctx context.Context, path string) (*introspection.Schema, string, error) {
	if path == "" {
		dag, err := dagger.Connect(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("connect to engine: %w", err)
		}
		defer dag.Close()
		schema, version, err := introspection.Introspect(ctx, dag)
		if err != nil {
			return nil, "", err
		}
		if schema == nil {
			return nil, "", fmt.Errorf("engine returned no schema")
		}
		return schema, version, nil
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

// checkPackageDir makes sure that dir exists and is the directory of the Go
// package with import path want. It finds the owning go.mod by searching up
// from dir.
func checkPackageDir(dir, want string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	root, err := findModuleRoot(dir)
	if err != nil {
		return err
	}
	got, err := packageImportPath(root, dir)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%s is package %s, not %s", dir, got, want)
	}
	return nil
}

// findModuleRoot returns the nearest directory at or above dir that contains
// a go.mod file.
func findModuleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := abs; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no go.mod found at or above %s", abs)
		}
	}
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

func subject(meta clientMeta) string {
	if meta.Subject != "" {
		return meta.Subject
	}
	return "the scope"
}

const checkLibraryUsage = `Usage: dagger-go-sdk-codegen check-library [flags]

Check, after go mod tidy, that the Go module at --module-root selects
dagger.io/dagger at exactly --target, naming the requirement that raised it
otherwise, and that its generated bindings alias only core names that
release has. A replace directive that applies to the target is a development
exception, reported on standard output.
`

func runCheckLibrary(args []string) error {
	flags := flag.NewFlagSet("check-library", flag.ContinueOnError)
	var (
		moduleRoot = flags.String("module-root", "", "root of the Go module")
		target     = flags.String("target", "", "the Dagger release the generated code targets")
		subject    = flags.String("subject", "the scope", "what the messages name, such as `module \"app\"`")
	)
	flags.Usage = func() { fmt.Fprint(flags.Output(), checkLibraryUsage) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *moduleRoot == "" || *target == "" {
		return fmt.Errorf("check-library needs --module-root and --target")
	}
	replaced, err := library.Replaced(filepath.Join(*moduleRoot, "go.mod"), *target)
	if err != nil {
		return err
	}
	if replaced {
		fmt.Println(library.ReplaceNote(*subject, *target))
		return nil
	}
	ctx := context.Background()
	if err := library.CheckSelection(ctx, *moduleRoot, *subject, *target); err != nil {
		return err
	}
	return library.CheckCoreNames(ctx, *moduleRoot, *subject, *target)
}
