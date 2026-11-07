package gogenerator

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

type PackageInfo struct {
	PackageName       string // Go package name, typically "main"
	PackageImport     string // import path of package in which this file appears
	DaggerPkgReplaced bool   // True `dagger.io/dagger` is replaced.
}

func loadPackage(ctx context.Context, dir string, allowEmpty bool) (*packages.Package, *token.FileSet, error) {
	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Context: ctx,
		Dir:     dir,
		Tests:   false,
		Fset:    fset,
		Mode: packages.NeedName |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedModule,
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			astFile, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			// strip function bodies since we don't need them and don't need to waste time in packages.Load with type checking them
			for _, decl := range astFile.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					fn.Body = nil
				}
			}
			return astFile, nil
		},
		// Print some debug logs with timing information to stdout
		Logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	}, ".")
	if err != nil {
		return nil, nil, err
	}
	switch len(pkgs) {
	case 0:
		return nil, nil, fmt.Errorf("no packages found in %s", dir)
	case 1:
		if pkgs[0].Name == "" && !allowEmpty {
			// this can happen when:
			// - loading an empty dir within an existing Go module
			// - loading a dir that is not included in a parent go.work
			return nil, nil, fmt.Errorf("package name is empty")
		}
		return pkgs[0], fset, nil
	default:
		// this would mean I don't understand how loading '.' works
		return nil, nil, fmt.Errorf("expected 1 package, got %d", len(pkgs))
	}
}

// stdlibImportErrors reports the errors of the standard library packages pkg
// imports. Their types come from export data, which a loader older than the
// Go toolchain cannot read. The declarations that use them would then have
// invalid types, and the generated code would describe them wrongly.
// Packages from modules are left to the build, which compiles the module
// without cgo, unlike the loader.
func stdlibImportErrors(pkg *packages.Package) error {
	var errs []error
	for _, path := range slices.Sorted(maps.Keys(pkg.Imports)) {
		imp := pkg.Imports[path]
		if imp.Module != nil || strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
			continue
		}
		for _, err := range imp.Errors {
			errs = append(errs, fmt.Errorf("import %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}
