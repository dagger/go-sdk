package templates

import (
	"go/types"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
)

// moduleNamer gives the schema names the engine gives the module's own
// declarations, where the generated code must spell them: the interface
// bindings select a module interface and its functions by schema name, and
// the dispatcher and Dang entrypoint must agree with the engine on the main
// object. The engine answers them itself (ModuleSource.naming, see
// identifiers.ModuleNames), by the rules of the version it serves the module
// at, on either side of the cut, so nothing here predicts them.
//
// This is the opposite direction of goNamer: goNamer styles schema names as
// Go identifiers, which is the SDK's choice; moduleNamer gives schema names
// of Go source names, which is the engine's.
type moduleNamer struct {
	naming identifiers.ModuleNames
}

// moduleNamer returns the namer of the module being generated.
func (funcs goTemplateFuncs) moduleNamer() moduleNamer {
	if funcs.cfg.ModuleConfig == nil {
		return moduleNamer{}
	}
	return moduleNamer{naming: funcs.cfg.ModuleConfig.Naming}
}

// answered returns an answer of the engine, or name when asking failed:
// generation then returns the failure (identifiers.ModuleNames.Err).
func answered(answer string, ok bool, name string) string {
	if !ok {
		return name
	}
	return answer
}

// FieldName is the schema name of a field, function or argument.
func (n moduleNamer) FieldName(name string) string {
	answer, ok := n.naming.FieldName(name)
	return answered(answer, ok, name)
}

// namespaceObject is the schema name of a type the module declares.
func (n moduleNamer) namespaceObject(typeName string) string {
	answer, ok := n.naming.TypeName(typeName)
	return answered(answer, ok, typeName)
}

// isMainObject reports whether the object named name is the module's main
// object, as the engine finds it among objects, the names of every object
// the module may declare (Module.objectTypeDefResultByOriginalName).
func (n moduleNamer) isMainObject(name string, objects []string) bool {
	main, ok := n.naming.MainObject(objects)
	return ok && main == name
}

// packageObjectNames lists the exported named struct types of a package: the
// objects a module may declare.
func packageObjectNames(pkg *types.Package) []string {
	if pkg == nil {
		return nil
	}
	var names []string
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() {
			continue
		}
		if _, ok := obj.Type().Underlying().(*types.Struct); ok {
			names = append(names, name)
		}
	}
	return names
}
