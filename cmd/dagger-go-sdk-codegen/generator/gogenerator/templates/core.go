package templates

import (
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// isCoreLibrary is true when generating the core bindings of dagger.io/dagger
// (the dagger.io/dagger/core package), as opposed to a standalone client.
func (funcs goTemplateFuncs) isCoreLibrary() bool {
	return funcs.cfg.CoreLibrary
}

// coreConstructorName returns the Go function name for a top-level Query
// field in the core library package. Query field names frequently match
// their own return type's name (e.g. "container" -> Container, returning
// *Container), which works fine as a free function in a separate package
// (see dagger.io/dagger/dag), but would redeclare the type if placed in the
// same package as the generated types. In that case, prefix the function with
// "New" instead.
func (funcs goTemplateFuncs) coreConstructorName(f introspection.Field) string {
	name := formatName(f.Name)
	if funcs.schema.Types.Get(name) != nil {
		return "New" + name
	}
	return name
}
