package gogenerator

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"go/token"
	"go/version"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"text/template"

	"github.com/iancoleman/strcase"
	"github.com/psanford/memfs"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"

	"codegen/generator"
	"codegen/generator/gogenerator/templates"
	"codegen/introspection"
)

const (
	// ClientGenFile is the path to write the codegen for the dagger API
	ClientGenFile = "dagger.gen.go"

	// internalDaggerDir is the directory where internal dagger generated files are written.
	internalDaggerDir = "internal/dagger"
)

var goVersion = strings.TrimPrefix(runtime.Version(), "go")

// develGoVersion pulls the language version out of a toolchain string
// go/version rejects, which in practice means a devel build: "devel
// go1.27-abcdef 2026-01-01".
var develGoVersion = regexp.MustCompile(`\bgo(\d+\.\d+)`)

// goLanguageVersion is the language version (major.minor) of a Go toolchain
// version, the go directive a fresh go.mod starts with. The engine writes its
// binary's full patch version; major.minor lets `go get` raise the directive
// to what a dependency needs, and keeps the builder image's patch level out of
// users' go.mod. semver cannot parse a release candidate ("go1.26rc1"), so the
// stdlib's own Go-version parser does it.
func goLanguageVersion(toolchain string) (string, error) {
	if lang := version.Lang(toolchain); lang != "" {
		return strings.TrimPrefix(lang, "go"), nil
	}
	if m := develGoVersion.FindStringSubmatch(toolchain); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("unrecognized Go toolchain version %q: cannot derive a go directive from it", toolchain)
}

type GoGenerator struct {
	Config generator.Config

	// libraryMod caches the pinned dagger.io/dagger go.mod across the module
	// bootstrap passes.
	libraryMod *modfile.File
}

// PackageInfo describes the Go package the generated files belong to.
type PackageInfo struct {
	PackageName   string // Go package name, "dagger" for a standalone client
	PackageImport string // import path of package in which this file appears

	DaggerPkgReplaced bool // whether `dagger.io/dagger` is replaced in go.mod
}

// moduleGenCtx carries the parsed module package into code generation. A nil
// *moduleGenCtx means client generation, which needs no parsed source.
type moduleGenCtx struct {
	pkg  *packages.Package
	fset *token.FileSet
	pass int
}

// templateFuncs selects the template FuncMap constructor by generation mode:
// client (mod == nil) or module (mod != nil, carrying the parsed source).
func templateFuncs(ctx context.Context, mod *moduleGenCtx, schema, fullSchema *introspection.Schema, schemaVersion string, cfg generator.Config) template.FuncMap {
	if mod == nil {
		return templates.GoTemplateFuncs(schema, fullSchema, schemaVersion, cfg)
	}
	return templates.GoTemplateFuncsForModule(ctx, schema, fullSchema, schemaVersion, cfg, mod.pkg, mod.fset, mod.pass)
}

// fullSchemaTemplates is the set of output file paths (without .tmpl suffix)
// that should be rendered against the full schema rather than the core schema
// (i.e. the schema with the bound module's types excluded). These templates
// need visibility into module-contributed Query fields (e.g. hello()) so that
// they can expose those constructors to callers.
var fullSchemaTemplates = map[string]bool{
	"dag/dag.gen.go": true,
}

func generateCode(
	ctx context.Context,
	cfg generator.Config,
	schema *introspection.Schema,
	schemaVersion string,
	mfs *memfs.FS,
	pkgInfo *PackageInfo,
	mod *moduleGenCtx, // nil => client generation
) error {
	// Collect all module names present in the schema so we can split them out
	// into separate files and exclude them from the main dagger.gen.go. The
	// client schema is core + the bound module only, so this yields one
	// <bound-module>.gen.go. In module generation the current module's own
	// types are merged into the schema with a @sourceMap for this module, so
	// they get their own <module>.gen.go too, same as dependencies.
	depNames := schema.DependencyNames()

	// When there are module-contributed types, generate the core schema
	// (excluding them) into the main dagger.gen.go, then generate each module
	// into its own file.
	coreSchema := schema
	if len(depNames) > 0 {
		coreSchema = schema.Exclude(depNames...)
	}

	// Build two template sets: one bound to the core schema (most files) and
	// one bound to the full schema (dag/dag.gen.go and other files that need
	// to expose module-contributed Query fields).
	coreFuncs := templateFuncs(ctx, mod, coreSchema, schema, schemaVersion, cfg)
	fullFuncs := templateFuncs(ctx, mod, schema, schema, schemaVersion, cfg)

	coreTmpls := templates.Templates(coreFuncs)
	fullTmpls := templates.Templates(fullFuncs)

	// Sort template keys for deterministic processing
	keys := make([]string, 0, len(coreTmpls))
	for k := range coreTmpls {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		// Choose the right template + schema pair for this output file.
		renderSchema := coreSchema
		tmpl := coreTmpls[k]
		if fullSchemaTemplates[k] {
			renderSchema = schema
			tmpl = fullTmpls[k]
		}

		dt, err := renderFile(cfg.OutputDir, renderSchema, schemaVersion, pkgInfo, tmpl)
		if err != nil {
			return err
		}
		if dt == nil {
			// no contents, skip
			continue
		}

		if err := mfs.MkdirAll(filepath.Dir(k), 0o755); err != nil {
			return err
		}
		if err := mfs.WriteFile(k, dt, 0600); err != nil {
			return err
		}
	}

	// Generate per-module files.
	if len(depNames) > 0 {
		if err := generateDependencyFiles(ctx, cfg, schema, schemaVersion, mfs, pkgInfo, mod, depNames); err != nil {
			return fmt.Errorf("generate dependency files: %w", err)
		}
	}

	return nil
}

// generateDependencyFiles generates one <module>.gen.go file per module found
// in the schema, each containing only the types contributed by that module.
// For a standalone client the schema contains exactly the bound module.
func generateDependencyFiles(
	ctx context.Context,
	cfg generator.Config,
	schema *introspection.Schema,
	schemaVersion string,
	mfs *memfs.FS,
	pkgInfo *PackageInfo,
	mod *moduleGenCtx,
	depNames []string,
) error {
	for _, depName := range depNames {
		depSchema := schema.Include(depName)

		funcs := templateFuncs(ctx, mod, depSchema, schema, schemaVersion, cfg)
		tmpl, err := templates.DepTemplate(funcs)
		if err != nil {
			return fmt.Errorf("get dependency template: %w", err)
		}

		dt, err := renderFile(cfg.OutputDir, depSchema, schemaVersion, pkgInfo, tmpl)
		if err != nil {
			return fmt.Errorf("render dependency file for %q: %w", depName, err)
		}
		if dt == nil {
			// no types for this module, skip
			continue
		}

		// Convert module name to kebab-case for the filename, e.g. "myDep" -> "my-dep.gen.go"
		depFileName := strcase.ToKebab(depName) + ".gen.go"

		// A standalone client's root package is "dagger", so its per-module
		// files live at the client root. A generated module's root is package
		// main, so its binding files go under internal/dagger (package dagger)
		// to avoid two packages in one directory.
		depFilePath := depFileName
		if cfg.ModuleConfig != nil {
			depFilePath = filepath.Join(internalDaggerDir, depFileName)
		}

		if err := mfs.MkdirAll(filepath.Dir(depFilePath), 0o755); err != nil {
			return err
		}
		if err := mfs.WriteFile(depFilePath, dt, 0600); err != nil {
			return fmt.Errorf("write dependency file %q: %w", depFilePath, err)
		}
	}

	return nil
}

func renderFile(
	outputDir string,
	schema *introspection.Schema,
	schemaVersion string,
	pkgInfo *PackageInfo,
	tmpl *template.Template,
) ([]byte, error) {
	data := struct {
		*PackageInfo
		Schema        *introspection.Schema
		SchemaVersion string
		Types         []*introspection.Type
	}{
		PackageInfo:   pkgInfo,
		Schema:        schema,
		SchemaVersion: schemaVersion,
		Types:         schema.Visit(),
	}

	var render bytes.Buffer
	if err := tmpl.Execute(&render, data); err != nil {
		return nil, err
	}

	source := render.Bytes()
	source = bytes.TrimSpace(source)
	if len(source) == 0 {
		return nil, nil
	}

	formatted, err := format.Source(source)
	if err != nil {
		os.Stderr.Write(source)
		return nil, fmt.Errorf("error formatting generated code: %w", err)
	}
	formatted, err = imports.Process(filepath.Join(outputDir, "dummy.go"), formatted, nil)
	if err != nil {
		os.Stderr.Write(source)
		return nil, fmt.Errorf("error processing imports in generated code: %w", err)
	}
	return formatted, nil
}
