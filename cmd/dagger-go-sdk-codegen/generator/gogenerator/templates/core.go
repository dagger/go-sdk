package templates

import (
	"slices"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
	"github.com/iancoleman/strcase"
)

// isCoreLibrary is true when generating the core bindings of dagger.io/dagger
// (the dagger.io/dagger/core package), as opposed to a standalone client.
func (funcs goTemplateFuncs) isCoreLibrary() bool {
	return funcs.cfg.CoreLibrary
}

// isUnifiedClient is true when generating a package that aliases the core
// types of dagger.io/dagger/core.
func (funcs goTemplateFuncs) isUnifiedClient() bool {
	return funcs.cfg.UnifiedClient
}

// coreConstructorName returns the Go function name for a top-level Query
// field in the core library package or in a unified client package. Query
// field names frequently match their own return type's name (e.g.
// "container" -> Container, returning *Container), which works fine as a free
// function in a separate package (see dagger.io/dagger/dag), but would
// redeclare the type if placed in the same package as the generated types. In
// that case, prefix the function with "New" instead.
//
// A unified client package also declares aliases of core types, so its
// fields are checked against the full schema. It returns "" when the name is
// still declared in that package; the method on Query remains available.
// The main constructor of a bound module is named New instead of New<Object>.
func (funcs goTemplateFuncs) coreConstructorName(f introspection.Field) string {
	name := formatName(f.Name)
	if !funcs.isUnifiedClient() {
		if funcs.schema.Types.Get(name) != nil {
			return "New" + name
		}
		return name
	}
	source := f.Directives.SourceMap()
	if funcs.isStandaloneClient() && source != nil && source.Module != "" &&
		formatName(strcase.ToLowerCamel(source.Module)) == name && f.TypeRef.IsObject() {
		if funcs.unifiedClientSchemaDeclares("New") {
			return ""
		}
		return "New"
	}
	if funcs.fullSchema.Types.Get(name) != nil {
		name = "New" + name
	}
	if funcs.unifiedClientDeclares(name) {
		return ""
	}
	return name
}

// unifiedClientNames are reserved for the shared client and its main module
// constructor. Keep them in sync with GenerateUnifiedClient and the shortcuts.
var unifiedClientNames = []string{
	"Client", "Query", "DaggerObject", "ExecError", "SetMarshalContext",
	"Connect", "New", "Ref", "Load",
}

// unifiedClientDeclares reports whether a unified client package can declare
// name at package level, either itself or through a core alias.
func (funcs goTemplateFuncs) unifiedClientDeclares(name string) bool {
	if slices.Contains(unifiedClientNames, name) {
		return true
	}
	return funcs.unifiedClientSchemaDeclares(name)
}

func (funcs goTemplateFuncs) unifiedClientSchemaDeclares(name string) bool {
	for _, t := range funcs.fullSchema.Types {
		typeName := formatName(t.Name)
		if name == typeName || name == typeName+"Client" || name == "With"+typeName+"Func" {
			return true
		}
		for _, field := range t.Fields {
			if funcs.hasOptionals(field.Args) && name == funcs.fieldOptionsStructName(*field) {
				return true
			}
		}
		for _, value := range t.EnumValues {
			if name == funcs.formatEnum(t.Name, value.Name) || name == funcs.formatEnum("", value.Name) {
				return true
			}
		}
	}
	return false
}
