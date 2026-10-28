package library

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// CheckCoreNames refuses generated bindings under root that alias a name the
// library's core package lacks at the targeted release. The bindings come
// from the running engine's schema, which can be newer than the target.
func CheckCoreNames(ctx context.Context, root, subject, target string) error {
	coreDir, err := goOutput(ctx, root, "list", "-f", "{{.Dir}}", Path+"/core")
	if err != nil {
		return err
	}
	have, err := exportedNames(coreDir)
	if err != nil {
		return err
	}
	bindings := filepath.Join(root, "internal", "dagger")
	clients, err := filepath.Glob(filepath.Join(bindings, "clients", "*", "dagger.gen.go"))
	if err != nil {
		return err
	}
	for _, path := range append([]string{filepath.Join(bindings, "dagger.gen.go")}, clients...) {
		missing, err := missingCoreNames(path, have)
		if err != nil {
			return err
		}
		if len(missing) == 0 {
			continue
		}
		user, verb, fix := "its code", "uses", "stop using"
		if filepath.Dir(path) != bindings {
			dependency := filepath.Base(filepath.Dir(path))
			user, verb, fix = fmt.Sprintf("dependency %q", dependency), "exposes", "use a version of "+dependency+" that does not expose"
		}
		it := "it"
		if len(missing) > 1 {
			it = "them"
		}
		return fmt.Errorf("%s targets Dagger %s, and %s %s %s, which that release does not have: set targetVersion to a release that has %s, such as the running engine's, in the scope's settings in dagger.toml, or %s %s", subject, target, user, verb, joinAnd(missing), it, fix, it)
	}
	return nil
}

func exportedNames(dir string) (map[string]bool, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	names := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if decl.Recv == nil && decl.Name.IsExported() {
						names[decl.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							if spec.Name.IsExported() {
								names[spec.Name.Name] = true
							}
						case *ast.ValueSpec:
							for _, name := range spec.Names {
								if name.IsExported() {
									names[name.Name] = true
								}
							}
						}
					}
				}
			}
		}
	}
	return names, nil
}

// missingCoreNames lists the names of the core package that the aliases of a
// generated binding file refer to and have is without, in order.
func missingCoreNames(path string, have map[string]bool) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
	if err != nil {
		return nil, err
	}
	var missing []string
	note := func(expr ast.Expr) {
		selector, ok := expr.(*ast.SelectorExpr)
		if !ok {
			return
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "core" || have[selector.Sel.Name] || slices.Contains(missing, selector.Sel.Name) {
			return
		}
		missing = append(missing, selector.Sel.Name)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			switch spec := spec.(type) {
			case *ast.TypeSpec:
				note(spec.Type)
			case *ast.ValueSpec:
				for _, value := range spec.Values {
					note(value)
				}
			}
		}
	}
	return missing, nil
}
