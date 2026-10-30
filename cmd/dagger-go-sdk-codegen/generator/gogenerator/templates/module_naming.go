package templates

import (
	"go/types"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
)

// moduleNamer gives the schema names the engine gives the module's own
// declarations: the names its self-call bindings select and its self-call
// schema declares. The engine answers them itself (ModuleSource.naming, see
// identifiers.ModuleNames), by the rules of the version it serves the module
// at, on either side of the cut. A name the bindings spell differently than
// the engine breaks the module, so nothing here predicts them.
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

// NewModuleNamer returns the names the engine gives a module's declarations,
// from its naming.
func NewModuleNamer(naming identifiers.ModuleNames) ModuleNamer {
	return moduleNamer{naming: naming}
}

// ModuleNamer is the part of the module namer that merging the self-call
// schema needs.
type ModuleNamer interface {
	// MainObjectName is the schema name of the module's main object.
	MainObjectName() string
	// FieldName is the schema name of a field, function or argument.
	FieldName(name string) string
}

// answered returns an answer of the engine, or, while it isn't answered yet,
// a placeholder no schema name can be mistaken for: identifiers.Run renders
// again once it's answered, so no placeholder reaches the output.
func answered(answer string, ok bool, name string) string {
	if !ok {
		return "<pending " + name + ">"
	}
	return answer
}

// FieldName is the schema name of a field, function or argument.
func (n moduleNamer) FieldName(name string) string {
	answer, ok := n.naming.FieldName(name)
	return answered(answer, ok, name)
}

// enumMemberName is the schema name of an enum member.
func (n moduleNamer) enumMemberName(name string) string {
	answer, ok := n.naming.EnumMemberName(name)
	return answered(answer, ok, name)
}

// MainObjectName is the schema name of the module's main object.
func (n moduleNamer) MainObjectName() string {
	answer, ok := n.naming.MainObjectName()
	return answered(answer, ok, "main object")
}

// namespaceObject is the schema name of a type the module declares.
func (n moduleNamer) namespaceObject(typeName string) string {
	answer, ok := n.naming.TypeName(typeName)
	return answered(answer, ok, typeName)
}

// typeName maps a type name a module declaration refers to, to the name the
// engine installs or resolves it under: a type the module declares
// (moduleName set) is namespaced, and a reference to a core or dependency
// type resolves as the engine resolves it (Module.depTypeReference).
func (n moduleNamer) typeName(name, moduleName string) string {
	if moduleName != "" {
		return n.namespaceObject(name)
	}
	answer, ok := n.naming.TypeReference(name)
	return answered(answer, ok, name)
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
