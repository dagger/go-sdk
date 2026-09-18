package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/iancoleman/strcase"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("render-template", flag.ContinueOnError)
	importable := flags.Bool("importable-package", false, "name the Go package after the module instead of main")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) != 3 {
		return fmt.Errorf("usage: render-template [--importable-package] MODULE_NAME TEMPLATE_DIR OUT_DIR")
	}

	moduleName := args[0]
	templateDir := args[1]
	outDir := args[2]
	// The Go runtime runs the module as a command, so it needs package main.
	// A module loaded through a Dang entrypoint is imported by its dispatch
	// command instead.
	packageName := "main"
	if *importable {
		packageName = modulePackage(moduleName)
	}
	data := map[string]string{
		"ModuleName":    moduleName,
		"ModuleType":    strcase.ToCamel(moduleName),
		"ModulePackage": packageName,
		"ModuleImport":  "dagger/" + strcase.ToKebab(moduleName),
	}

	return filepath.WalkDir(templateDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(templateDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		dstRel := strings.TrimSuffix(rel, ".tmpl")
		dst := filepath.Join(outDir, dstRel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("template symlinks are not supported: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(rel, ".tmpl") {
			return os.WriteFile(dst, contents, 0o644)
		}

		var buf bytes.Buffer
		tmpl, err := template.New(rel).Parse(string(contents))
		if err != nil {
			return err
		}
		if err := tmpl.Execute(&buf, data); err != nil {
			return err
		}
		return os.WriteFile(dst, buf.Bytes(), 0o644)
	})
}

func modulePackage(moduleName string) string {
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
