package templates

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// moduleNamer gives the schema names the engine gives the module's own
// declarations, where the generated code must spell them: the interface
// bindings select a module type and its functions by schema name, and the
// dispatcher and Dang entrypoint must agree with the engine on the main
// object. The engine answers them itself (ModuleSource.naming), up front,
// for the names ModuleDeclarations lists, so nothing here predicts them.
//
// This is the opposite direction of goNamer: goNamer styles schema names as
// Go identifiers, which is the SDK's choice; moduleNamer gives schema names
// of Go source names, which is the engine's.
type moduleNamer struct {
	names identifiers.ModuleNames
}

// moduleNamer returns the namer of the module being generated.
func (funcs goTemplateFuncs) moduleNamer() moduleNamer {
	if funcs.cfg.ModuleConfig == nil {
		return moduleNamer{}
	}
	return moduleNamer{names: funcs.cfg.ModuleConfig.Naming}
}

// answer returns the engine's answer for name. Every name the generated code
// spells was asked for (ModuleDeclarations): one that wasn't is a bug.
func answer(answers map[string]string, kind, name string) string {
	answer, ok := answers[name]
	if !ok {
		panic(fmt.Sprintf("the engine wasn't asked for the schema name of %s %q (templates.ModuleDeclarations)", kind, name))
	}
	return answer
}

// FieldName is the schema name of a field, function or argument.
func (n moduleNamer) FieldName(name string) string {
	return answer(n.names.Fields, "field", name)
}

// namespaceObject is the schema name of a type the module declares.
func (n moduleNamer) namespaceObject(typeName string) string {
	return answer(n.names.Types, "type", typeName)
}

// isMainObject reports whether the object named name is the module's main
// object.
func (n moduleNamer) isMainObject(name string) bool {
	return name != "" && n.names.Main == name
}

// ModuleDeclarations lists the names of a module's package the engine names
// for its generated code (identifiers.NameModule): the exported named struct
// and interface types the module declares, and the structs again as the
// candidates for the main object; for each interface that embeds
// DaggerObject, ID and its other methods, with their arguments as the parser
// names them. Generated code declares nothing.
func ModuleDeclarations(pkg *packages.Package, fset *token.FileSet, schema *introspection.Schema, moduleName string) (typeNames, fieldNames, candidates []string) {
	ps := &parseState{pkg: pkg, fset: fset, schema: schema, moduleName: moduleName}
	scope := pkg.Types.Scope()
	var daggerObject []string
	if obj, ok := scope.Lookup(daggerObjectIfaceName).(*types.TypeName); ok {
		if iface, ok := obj.Type().Underlying().(*types.Interface); ok {
			for method := range iface.Methods() {
				daggerObject = append(daggerObject, method.Name())
			}
		}
	}
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() || ps.isDaggerGenerated(obj) {
			continue
		}
		named, _ := obj.Type().(*types.Named)
		switch underlying := obj.Type().Underlying().(type) {
		case *types.Struct:
			typeNames = append(typeNames, name)
			candidates = append(candidates, name)
		case *types.Interface:
			if named == nil || name == daggerObjectIfaceName || !embedsAll(underlying, daggerObject) {
				continue
			}
			typeNames = append(typeNames, name)
			fieldNames = append(fieldNames, "ID")
			for method := range underlying.Methods() {
				if !method.Exported() || slices.Contains(daggerObject, method.Name()) {
					continue
				}
				fieldNames = append(fieldNames, method.Name())
				// A method the parser refuses fails generation when it's
				// parsed again; it has no arguments to name.
				specs, _ := ps.parseParamSpecs(named, method)
				for _, spec := range specs {
					if !spec.isContext {
						fieldNames = append(fieldNames, spec.name)
					}
				}
			}
		}
	}
	return typeNames, fieldNames, candidates
}

// embedsAll reports whether iface has every method named in methods.
func embedsAll(iface *types.Interface, methods []string) bool {
	for _, name := range methods {
		if obj, _, _ := types.LookupFieldOrMethod(iface, false, nil, name); obj == nil {
			return false
		}
	}
	return true
}
