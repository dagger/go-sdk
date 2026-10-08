package gogenerator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/iancoleman/strcase"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	clientgen "github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator/gogenerator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator/gogenerator/templates"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

type GenerateConfig struct {
	ModuleRoot    string
	ModuleName    string
	SchemaPath    string
	SchemaVersion string
	DaggerVersion string
	GoImage       string
	// RemovedPath, when set, names a file that receives the module-relative
	// paths of the generated files this run removed, one per line. The caller
	// merges the remaining files back, so it needs the list to remove the rest.
	RemovedPath   string
	UnifiedClient bool
	// GlobalClient is auto, true, or false. Auto preserves recognized legacy
	// projects and leaves new modules without an unqualified global dag API.
	GlobalClient string
	globalClient bool
}

func generate(ctx context.Context, cfg GenerateConfig) error {
	root, err := filepath.Abs(cfg.ModuleRoot)
	if err != nil {
		return fmt.Errorf("resolve module root: %w", err)
	}

	// Check the package before anything is written, so a refused module is
	// left as it was.
	packageName, err := sourcePackageName(root)
	if err != nil {
		return err
	}
	dispatchPath := filepath.Join(root, "cmd", strcase.ToKebab(cfg.ModuleName)+"-dispatch", "main.go")
	if packageName == "main" {
		dispatchPath = filepath.Join(root, "dagger.dispatch.gen.go")
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); errors.Is(err, os.ErrNotExist) {
		for parent := filepath.Dir(root); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			if _, err := os.Stat(filepath.Join(parent, "go.mod")); err == nil {
				return fmt.Errorf("Dang entrypoints need a go.mod in the module directory; keep the Go runtime for modules using a parent go.mod")
			}
		}
	}
	entrypointDir := filepath.Join(root, "internal", "dagger", "entrypoint")
	owned := []string{
		filepath.Join(root, "dagger.gen.go"),
		dispatchPath,
		filepath.Join(entrypointDir, "main.dang"),
	}
	for _, path := range owned {
		if err := checkGeneratedPath(root, path); err != nil {
			return err
		}
	}
	packageImport, goModPath, err := ensureGoModule(root, cfg.ModuleName)
	if err != nil {
		return err
	}
	moduleSubpath, err := filepath.Rel(filepath.Dir(goModPath), root)
	if err != nil {
		return fmt.Errorf("resolve module path relative to go.mod: %w", err)
	}
	moduleSubpath = filepath.ToSlash(moduleSubpath)
	if err := pinDagger(goModPath, cfg.DaggerVersion); err != nil {
		return err
	}
	runtimeVersion := cfg.DaggerVersion
	// Bootstrap bindings do not import the runtime. Remember an existing
	// higher requirement before tidy can remove it as temporarily unused.
	modData, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	mod, err := modfile.Parse(goModPath, modData, nil)
	if err != nil {
		return err
	}
	for _, required := range mod.Require {
		if required.Mod.Path == "dagger.io/dagger" && semver.Compare(required.Mod.Version, runtimeVersion) > 0 {
			runtimeVersion = required.Mod.Version
		}
	}
	if err := pinGeneratedTelemetry(goModPath); err != nil {
		return err
	}
	if err := removeLegacyGeneratedFile(root); err != nil {
		return err
	}

	resp, err := readSchema(cfg.SchemaPath, cfg.SchemaVersion)
	if err != nil {
		return err
	}
	cfg.SchemaVersion = resp.SchemaVersion
	generator.SetSchemaParents(resp.Schema)

	genCfg := generator.Config{
		OutputDir:     root,
		UnifiedClient: cfg.UnifiedClient,
		GlobalClient:  cfg.globalClient,
		ClientConfig:  &generator.ClientGeneratorConfig{},
		ModuleConfig: &generator.ModuleGeneratorConfig{
			ModuleName: cfg.ModuleName,
			LibVersion: cfg.DaggerVersion,
		},
	}
	client := &clientgen.GoGenerator{Config: genCfg}
	// Author source can reference any core alias before its self-call schema
	// has been discovered. The bootstrap supplies the complete legacy surface.
	client.Config.UnifiedClient = false
	if _, err := generateClient(ctx, client, resp.Schema, cfg.SchemaVersion, packageImport, root, false); err != nil {
		return fmt.Errorf("bootstrap module client: %w", err)
	}
	if err := writeBootstrap(root, packageName, packageImport, cfg.globalClient); err != nil {
		return err
	}
	if err := goModTidy(ctx, root); err != nil {
		return err
	}

	pkg, fset, err := loadPackage(ctx, root, false)
	if err != nil {
		return fmt.Errorf("load module package: %w", err)
	}
	emitter := templates.NewModuleIntrospectionEmitter(ctx, resp.Schema, cfg.SchemaVersion, genCfg, pkg, fset)
	moduleJSON, err := emitter.ModuleIntrospectionJSON(cfg.ModuleName)
	if err != nil {
		return fmt.Errorf("analyze module types: %w", err)
	}
	merged, err := mergeSchema(resp, moduleJSON, cfg.ModuleName)
	if err != nil {
		return fmt.Errorf("merge self-call schema: %w", err)
	}
	generator.SetSchemaParents(merged.Schema)
	client.Config.UnifiedClient = cfg.UnifiedClient
	removed, err := generateClient(ctx, client, merged.Schema, cfg.SchemaVersion, packageImport, root, true)
	if err != nil {
		return fmt.Errorf("generate module client: %w", err)
	}
	// The bootstrap has no runtime SDK import, so its tidy may remove the
	// requirement. Restore the caller's pin before unified bindings import core.
	if err := pinDagger(goModPath, runtimeVersion); err != nil {
		return err
	}
	if err := goModTidy(ctx, root); err != nil {
		return err
	}

	pkg, fset, err = loadPackage(ctx, root, false)
	if err != nil {
		return fmt.Errorf("reload module package: %w", err)
	}
	artifacts, err := templates.GenerateV2Artifacts(
		ctx, merged.Schema, cfg.SchemaVersion, genCfg, pkg, fset, packageImport, moduleSubpath, cfg.GoImage,
	)
	if err != nil {
		return fmt.Errorf("generate manifest-v2 artifacts: %w", err)
	}

	if err := writeFile(filepath.Join(root, "dagger.gen.go"), artifacts.ModuleSource); err != nil {
		return err
	}
	if err := writeFile(dispatchPath, artifacts.DispatchSource); err != nil {
		return err
	}
	runtimeFiles, err := collectionBuildSources(ctx, root)
	if err != nil {
		return fmt.Errorf("prepare collection build: %w", err)
	}
	artifacts.EntrypointSource, err = writeCollectionBuild(root, artifacts.EntrypointSource, runtimeFiles)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(entrypointDir, "main.dang"), artifacts.EntrypointSource); err != nil {
		return err
	}
	if err := goModTidy(ctx, root); err != nil {
		return err
	}
	if err := normalizeGoMod(goModPath); err != nil {
		return fmt.Errorf("normalize generated go.mod: %w", err)
	}
	// Introspection loads declarations without function bodies. Compile the
	// completed dispatcher before committing the staged tree, so a migration
	// that removes an API still used by author code leaves the project intact.
	target := "./cmd/" + strcase.ToKebab(cfg.ModuleName) + "-dispatch"
	if packageName == "main" {
		target = "."
	}
	if err := compileGeneratedModule(ctx, root, target, runtimeFiles); err != nil {
		return err
	}
	if cfg.RemovedPath != "" {
		var list bytes.Buffer
		for _, rel := range removed {
			list.WriteString(rel + "\n")
		}
		if err := os.WriteFile(cfg.RemovedPath, list.Bytes(), 0o644); err != nil {
			return fmt.Errorf("write removed file list: %w", err)
		}
	}
	return nil
}

// generateClient writes the embedded client. With removeStale, it also removes
// the generated bindings the client no longer has, and returns their paths
// relative to root.
func generateClient(ctx context.Context, gen *clientgen.GoGenerator, schema *introspection.Schema, schemaVersion, packageImport, root string, removeStale bool) ([]string, error) {
	state, err := gen.GenerateEmbeddedClient(ctx, schema, schemaVersion, packageImport+"/internal/dagger")
	if err != nil {
		return nil, err
	}
	var removed []string
	if removeStale {
		removed, err = removeStaleBindings(root, state.Overlay)
		if err != nil {
			return nil, err
		}
	}
	return removed, generator.Overlay(ctx, state.Overlay, root)
}

func removeStaleBindings(root string, overlay fs.FS) ([]string, error) {
	dir := filepath.Join(root, "internal", "dagger")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || name == "dagger.gen.go" || !strings.HasSuffix(name, ".gen.go") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("internal", "dagger", name))
		if _, err := fs.Stat(overlay, rel); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !bytes.HasPrefix(data, []byte("// Code generated by dagger. DO NOT EDIT.")) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale binding %s: %w", path, err)
		}
		removed = append(removed, rel)
	}
	return removed, nil
}

func readSchema(path, version string) (*introspection.Response, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read introspection JSON: %w", err)
	}
	var resp introspection.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode introspection JSON: %w", err)
	}
	if resp.Schema == nil {
		var schema introspection.Schema
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, fmt.Errorf("decode raw introspection schema: %w", err)
		}
		if len(schema.Types) == 0 {
			return nil, fmt.Errorf("introspection JSON has no __schema")
		}
		resp.Schema = &schema
	}
	if version != "" {
		resp.SchemaVersion = version
	}
	if resp.SchemaVersion == "" {
		resp.SchemaVersion = "v1.0.0-beta.11"
	}
	return &resp, nil
}

func ensureGoModule(root, moduleName string) (packageImport, goModPath string, _ error) {
	dir := root
	for {
		path := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(path)
		if err == nil {
			mod, err := modfile.Parse(path, data, nil)
			if err != nil {
				return "", "", fmt.Errorf("parse %s: %w", path, err)
			}
			if mod.Module == nil {
				return "", "", fmt.Errorf("%s has no module directive", path)
			}
			rel, err := filepath.Rel(dir, root)
			if err != nil {
				return "", "", err
			}
			return filepath.ToSlash(filepath.Join(mod.Module.Mod.Path, rel)), path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("read %s: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	mod := new(modfile.File)
	if err := mod.AddModuleStmt("dagger/" + strcase.ToKebab(moduleName)); err != nil {
		return "", "", err
	}
	if err := mod.AddGoStmt("1.26"); err != nil {
		return "", "", err
	}
	body, err := mod.Format()
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(root, "go.mod")
	if err := writeFile(path, body); err != nil {
		return "", "", err
	}
	return "dagger/" + strcase.ToKebab(moduleName), path, nil
}

func pinDagger(path, version string) error {
	if version == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	mod, err := modfile.Parse(path, data, nil)
	if err != nil {
		return err
	}
	for _, replace := range mod.Replace {
		if replace.Old.Path == "dagger.io/dagger" {
			return nil
		}
	}
	for _, require := range mod.Require {
		if require.Mod.Path == "dagger.io/dagger" && semver.Compare(require.Mod.Version, version) >= 0 {
			return nil
		}
	}
	// A development engine reports an unreleased version. Pinning it would
	// leave a requirement that go mod tidy cannot resolve.
	if !moduleVersionExists(filepath.Dir(path), "dagger.io/dagger", version) {
		return nil
	}
	if err := mod.AddRequire("dagger.io/dagger", version); err != nil {
		return err
	}
	body, err := mod.Format()
	if err != nil {
		return err
	}
	return writeFile(path, body)
}

// The embedded transport needs only propagation from otel-go, but that package
// also imports the experimental log SDK. Seed its known dependency graph before
// tidy discovers direct OTel imports: resolving those imports independently at
// latest can select a log API that the SDK in otel-go no longer compiles with.
// This runtime dependency is independent of the module's schema version.
func pinGeneratedTelemetry(path string) error {
	const modulePath = "github.com/dagger/otel-go"
	const version = "v1.43.0"
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	mod, err := modfile.Parse(path, data, nil)
	if err != nil {
		return err
	}
	for _, replacement := range mod.Replace {
		if replacement.Old.Path == modulePath {
			return nil
		}
	}
	for _, requirement := range mod.Require {
		if requirement.Mod.Path == modulePath && semver.Compare(requirement.Mod.Version, version) >= 0 {
			return nil
		}
	}
	if err := mod.AddRequire(modulePath, version); err != nil {
		return err
	}
	body, err := mod.Format()
	if err != nil {
		return err
	}
	return writeFile(path, body)
}

// moduleVersionExists reports whether the module proxy can resolve a version.
// A failure to reach the proxy reads as "cannot pin", like the client helper.
func moduleVersionExists(dir, modulePath, version string) bool {
	cmd := exec.Command("go", "list", "-m", "-e", "-f", "{{if .Error}}unresolved{{end}}", modulePath+"@"+version)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == ""
}

// generatedMarker matches the header of a file that a generator owns: Go's
// convention, and the Dang entrypoint's comment.
var generatedMarker = regexp.MustCompile(`(?mi)^(//|#) Code generated .* do not edit\.$`)

// checkGeneratedPath refuses to overwrite a file that the generator does not
// own. A missing file, or one with a generated-code header, can be written.
func checkGeneratedPath(root, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if generatedMarker.Match(data) {
		return nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return fmt.Errorf("%s exists and is not generated code; the SDK generates this file, so move or rename it and generate again", filepath.ToSlash(rel))
}

func removeLegacyGeneratedFile(root string) error {
	path := filepath.Join(root, "dagger.gen.go")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !generatedMarker.Match(data) {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove old generated dispatcher: %w", err)
	}
	return nil
}

func goModTidy(ctx context.Context, root string) error {
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// compileGeneratedModule checks the complete dispatcher, including author
// function bodies that declaration-only package analysis intentionally skips.
// Collection modules compile against the private runtime source overlays that
// the generated entrypoint applies before its own build.
func compileGeneratedModule(ctx context.Context, root, target string, runtimeFiles map[string][]byte) error {
	buildRoot := root
	if len(runtimeFiles) > 0 {
		var err error
		buildRoot, err = os.MkdirTemp("", "dagger-module-compile-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(buildRoot)
		if err := os.CopyFS(buildRoot, os.DirFS(root)); err != nil {
			return fmt.Errorf("stage module compilation: %w", err)
		}
		for name, data := range runtimeFiles {
			if err := writeFile(filepath.Join(buildRoot, name), data); err != nil {
				return err
			}
		}
	}

	cmd := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", os.DevNull, target)
	cmd.Dir = buildRoot
	for _, setting := range os.Environ() {
		if !strings.HasPrefix(setting, "CGO_ENABLED=") {
			cmd.Env = append(cmd.Env, setting)
		}
	}
	cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compile generated module: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// The temporary embedded client can promote runtime dependencies to direct
// requirements. Final tidy demotes them again, retaining different require
// blocks depending on the input layout. Normalize only after all final sources
// have been written so repeated generation produces the same go.mod.
func normalizeGoMod(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	mod, err := modfile.Parse(path, data, nil)
	if err != nil {
		return err
	}
	// x/mod keeps requirement-line comments when regrouping, but drops
	// comments on parentheses when an emptied block is cleaned up. Preserve
	// those notes as block documentation before consolidating requirements.
	for _, statement := range mod.Syntax.Stmt {
		block, ok := statement.(*modfile.LineBlock)
		if !ok || len(block.Token) == 0 || block.Token[0] != "require" {
			continue
		}
		notes := block.Before
		for _, comments := range []modfile.Comments{
			{Suffix: block.Suffix}, block.LParen.Comments,
			block.RParen.Comments, {After: block.After},
		} {
			notes = append(notes, comments.Before...)
			notes = append(notes, comments.Suffix...)
			notes = append(notes, comments.After...)
		}
		for i := range notes {
			notes[i].Suffix = false
		}
		block.Comments = modfile.Comments{Before: notes}
		block.LParen.Comments = modfile.Comments{}
		block.RParen.Comments = modfile.Comments{}
	}
	mod.SetRequireAtMostTwo(mod.Require)
	mod.Cleanup()
	data, err = mod.Format()
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func defaultPackageName(moduleName string) string {
	name := strings.ReplaceAll(strcase.ToKebab(moduleName), "-", "_")
	if name == "" {
		return "module"
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "module_" + name
	}
	if token.IsKeyword(name) {
		name += "_module"
	}
	return name
}

func sourcePackageName(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	packageName := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || entry.Name() == "dagger.gen.go" {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(root, entry.Name()), nil, parser.PackageClauseOnly)
		if err != nil {
			return "", fmt.Errorf("parse package in %s: %w", entry.Name(), err)
		}
		if packageName == "" {
			packageName = file.Name.Name
			continue
		}
		if file.Name.Name != packageName {
			return "", fmt.Errorf("module root has packages %s and %s", packageName, file.Name.Name)
		}
	}
	if packageName == "" {
		return "", fmt.Errorf("module root has no Go source")
	}
	return packageName, nil
}

func writeBootstrap(root, packageName, packageImport string, globalClient bool) error {
	global := ""
	if globalClient {
		global = "var dag = dagger.Connect()\n"
	}
	source := fmt.Sprintf(`// Code generated by dagger. DO NOT EDIT.

package %s

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"github.com/dagger/querybuilder"

	"%s/internal/dagger"
)

%s

func Tracer() trace.Tracer { return otel.Tracer("dagger.io/sdk.go") }

type DaggerObject interface {
	querybuilder.GraphQLMarshaller
	ID(ctx context.Context) (dagger.ID, error)
}

type ExecError = dagger.ExecError
`, packageName, packageImport, global)
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return fmt.Errorf("format bootstrap dispatcher: %w", err)
	}
	return writeFile(filepath.Join(root, "dagger.gen.go"), formatted)
}
