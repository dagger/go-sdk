package gogenerator

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/template"

	"github.com/iancoleman/strcase"
	"github.com/psanford/memfs"
	"golang.org/x/tools/imports"

	"codegen/generator"
	"codegen/generator/gogenerator/templates"
	"codegen/introspection"
)

const (
	// ClientGenFile is the path to write the codegen for the dagger API
	ClientGenFile = "dagger.gen.go"
)

var goVersion = strings.TrimPrefix(runtime.Version(), "go")

type GoGenerator struct {
	Config generator.Config
}

// PackageInfo describes the Go package the generated files belong to.
type PackageInfo struct {
	PackageName   string // Go package name, "dagger" for a standalone client
	PackageImport string // import path of package in which this file appears
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
) error {
	// Collect all module names present in the schema so we can split them out
	// into separate files and exclude them from the main dagger.gen.go. The
	// client schema is core + the bound module only, so this yields one
	// <bound-module>.gen.go.
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
	coreFuncs := templates.GoTemplateFuncs(coreSchema, schema, schemaVersion, cfg)
	fullFuncs := templates.GoTemplateFuncs(schema, schema, schemaVersion, cfg)

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
		if err := generateDependencyFiles(ctx, cfg, schema, schemaVersion, mfs, pkgInfo, depNames); err != nil {
			return fmt.Errorf("generate dependency files: %w", err)
		}
	}

	return nil
}

// generateDependencyFiles generates one <module>.gen.go file per module found
// in the schema, each containing only the types contributed by that module.
// For a standalone client the schema contains exactly the bound module.
func generateDependencyFiles(
	_ context.Context,
	cfg generator.Config,
	schema *introspection.Schema,
	schemaVersion string,
	mfs *memfs.FS,
	pkgInfo *PackageInfo,
	depNames []string,
) error {
	for _, depName := range depNames {
		depSchema := schema.Include(depName)

		funcs := templates.GoTemplateFuncs(depSchema, schema, schemaVersion, cfg)
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
		depFilePath := strcase.ToKebab(depName) + ".gen.go"

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
