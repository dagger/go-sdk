package templates

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
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
// and interface types the module declares, the methods of the interfaces
// (DaggerObject's but ID left out) and their parameters, and the structs
// again as the candidates for the main object. Generated code declares
// nothing.
func ModuleDeclarations(pkg *packages.Package, fset *token.FileSet) (typeNames, fieldNames, candidates []string) {
	ps := &parseState{fset: fset}
	scope := pkg.Types.Scope()
	daggerObject := map[string]bool{}
	if obj, ok := scope.Lookup(daggerObjectIfaceName).(*types.TypeName); ok {
		if iface, ok := obj.Type().Underlying().(*types.Interface); ok {
			for method := range iface.Methods() {
				daggerObject[method.Name()] = true
			}
		}
	}
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() || ps.isDaggerGenerated(obj) {
			continue
		}
		switch underlying := obj.Type().Underlying().(type) {
		case *types.Struct:
			typeNames = append(typeNames, name)
			candidates = append(candidates, name)
		case *types.Interface:
			typeNames = append(typeNames, name)
			fieldNames = append(fieldNames, "ID")
			for method := range underlying.Methods() {
				if !method.Exported() || daggerObject[method.Name()] {
					continue
				}
				fieldNames = append(fieldNames, method.Name())
				fieldNames = append(fieldNames, paramNames(method.Signature().Params())...)
			}
		}
	}
	return typeNames, fieldNames, candidates
}

// paramNames are the names of params, and the fields of an inline struct
// param, which the bindings pass as arguments.
func paramNames(params *types.Tuple) []string {
	var names []string
	for param := range params.Variables() {
		if inline, ok := param.Type().(*types.Struct); ok {
			for field := range inline.Fields() {
				names = append(names, field.Name())
			}
			continue
		}
		if name := param.Name(); name != "" && name != "_" {
			names = append(names, name)
		}
	}
	return names
}
