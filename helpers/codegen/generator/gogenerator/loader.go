package gogenerator

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"golang.org/x/tools/go/packages"
)

// loadPackage type-checks the module package at dir with function bodies
// stripped: signatures are all module generation reads, and skipping bodies
// keeps packages.Load cheap.
//
// packages.Load runs `go list -e`, so it returns a package even when loading
// went wrong. A file that does not parse, or an import whose module cannot be
// resolved, is fatal: the emitter would otherwise see `invalid type` where the
// user wrote a real type and silently drop those functions. The root package
// only records an unresolvable import as a TypeError ("could not import"); the
// ListError naming the missing module sits on the imported stub, which is why
// NeedImports is requested without NeedDeps. Other TypeErrors are tolerated,
// as the engine tolerates them — stripping bodies makes "imported and not
// used" routine on a perfectly ordinary module.
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
		if err := fatalPackageErrors(pkgs[0]); err != nil {
			return nil, nil, err
		}
		return pkgs[0], fset, nil
	default:
		// this would mean I don't understand how loading '.' works
		return nil, nil, fmt.Errorf("expected 1 package, got %d", len(pkgs))
	}
}

func fatalPackageErrors(pkg *packages.Package) error {
	var errs []error
	for _, e := range pkg.Errors {
		switch e.Kind {
		case packages.ListError, packages.ParseError:
			errs = append(errs, errors.New(e.Error()))
		}
	}
	for _, imp := range pkg.Imports {
		for _, e := range imp.Errors {
			if e.Kind == packages.ListError {
				errs = append(errs, errors.New(e.Error()))
			}
		}
	}
	return errors.Join(errs...)
}
