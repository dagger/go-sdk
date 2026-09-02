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
	"strings"

	"github.com/iancoleman/strcase"
	"golang.org/x/mod/modfile"

	"codegen/generator"
	clientgen "codegen/generator/gogenerator"
	"codegen/generator/gogenerator/templates"
	"codegen/introspection"
	"module-codegen/manifest"
)

type GenerateConfig struct {
	ModuleRoot           string
	ModuleName           string
	SchemaPath           string
	SchemaVersion        string
	DaggerVersion        string
	GoImage              string
	CoreOnly             bool
	RemoveLegacyManifest bool
}

func Generate(ctx context.Context, cfg GenerateConfig) error {
	root, err := filepath.Abs(cfg.ModuleRoot)
	if err != nil {
		return fmt.Errorf("resolve module root: %w", err)
	}

	packageImport, goModPath, err := ensureGoModule(root, cfg.ModuleName)
	if err != nil {
		return err
	}
	packageName, err := sourcePackageName(root)
	if err != nil {
		return err
	}
	if packageName == "main" {
		return fmt.Errorf("manifest v2 requires an importable Go package; change package main to package %s", defaultPackageName(cfg.ModuleName))
	}
	moduleSubpath, err := filepath.Rel(filepath.Dir(goModPath), root)
	if err != nil {
		return fmt.Errorf("resolve module path relative to go.mod: %w", err)
	}
	moduleSubpath = filepath.ToSlash(moduleSubpath)
	if err := pinDagger(goModPath, cfg.DaggerVersion); err != nil {
		return err
	}
	if err := removeLegacyGeneratedFile(root); err != nil {
		return err
	}

	resp, err := readSchema(cfg.SchemaPath, cfg.SchemaVersion)
	if err != nil {
		return err
	}
	if cfg.CoreOnly {
		resp.Schema = resp.Schema.Exclude(resp.Schema.DependencyNames()...)
	}
	cfg.SchemaVersion = resp.SchemaVersion
	generator.SetSchemaParents(resp.Schema)

	genCfg := generator.Config{
		OutputDir:    root,
		ClientConfig: &generator.ClientGeneratorConfig{},
		ModuleConfig: &generator.ModuleGeneratorConfig{
			ModuleName: cfg.ModuleName,
			LibVersion: cfg.DaggerVersion,
		},
	}
	client := &clientgen.GoGenerator{Config: genCfg}
	if err := generateClient(ctx, client, resp.Schema, cfg.SchemaVersion, packageImport, root, false); err != nil {
		return fmt.Errorf("bootstrap module client: %w", err)
	}
	if err := writeBootstrap(root, packageName, packageImport); err != nil {
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
	if err := generateClient(ctx, client, merged.Schema, cfg.SchemaVersion, packageImport, root, true); err != nil {
		return fmt.Errorf("generate module client: %w", err)
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
	dispatchDir := filepath.Join(root, "cmd", strcase.ToKebab(cfg.ModuleName)+"-dispatch")
	if err := writeFile(filepath.Join(dispatchDir, "main.go"), artifacts.DispatchSource); err != nil {
		return err
	}
	entrypointDir := filepath.Join(root, "internal", "dagger", "entrypoint")
	if err := writeFile(filepath.Join(entrypointDir, "main.dang"), artifacts.EntrypointSource); err != nil {
		return err
	}
	moduleManifest := manifest.New(cfg.ModuleName).
		WithEntrypoint(manifest.DangKind, "./internal/dagger/entrypoint")
	manifestFile, err := moduleManifest.AsFile()
	if err != nil {
		return fmt.Errorf("render module manifest: %w", err)
	}
	if err := writeFile(filepath.Join(root, "dagger-module.toml"), manifestFile); err != nil {
		return err
	}
	if cfg.RemoveLegacyManifest {
		if err := os.Remove(filepath.Join(root, "dagger.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove legacy manifest: %w", err)
		}
	}
	if err := goModTidy(ctx, root); err != nil {
		return err
	}
	return nil
}

func generateClient(ctx context.Context, gen *clientgen.GoGenerator, schema *introspection.Schema, schemaVersion, packageImport, root string, removeStale bool) error {
	state, err := gen.GenerateEmbeddedClient(ctx, schema, schemaVersion, packageImport+"/internal/dagger")
	if err != nil {
		return err
	}
	if removeStale {
		if err := removeStaleBindings(root, state.Overlay); err != nil {
			return err
		}
	}
	return generator.Overlay(ctx, state.Overlay, root)
}

func removeStaleBindings(root string, overlay fs.FS) error {
	dir := filepath.Join(root, "internal", "dagger")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || name == "dagger.gen.go" || !strings.HasSuffix(name, ".gen.go") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("internal", "dagger", name))
		if _, err := fs.Stat(overlay, rel); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(data, []byte("// Code generated by dagger. DO NOT EDIT.")) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale binding %s: %w", path, err)
		}
	}
	return nil
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
	if err := mod.AddRequire("dagger.io/dagger", version); err != nil {
		return err
	}
	body, err := mod.Format()
	if err != nil {
		return err
	}
	return writeFile(path, body)
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
	if !bytes.HasPrefix(data, []byte("// Code generated by dagger. DO NOT EDIT.")) {
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

func writeBootstrap(root, packageName, packageImport string) error {
	source := fmt.Sprintf(`// Code generated by dagger. DO NOT EDIT.

package %s

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"github.com/dagger/querybuilder"

	"%s/internal/dagger"
)

var dag = dagger.Connect()

func Tracer() trace.Tracer { return otel.Tracer("dagger.io/sdk.go") }

type DaggerObject interface {
	querybuilder.GraphQLMarshaller
	ID(ctx context.Context) (dagger.ID, error)
}

type ExecError = dagger.ExecError
`, packageName, packageImport)
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return fmt.Errorf("format bootstrap dispatcher: %w", err)
	}
	return writeFile(filepath.Join(root, "dagger.gen.go"), formatted)
}
