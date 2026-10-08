package templates

import (
	"strings"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// boundModule returns the single module the generated client serves. For a
// local module (LOCAL_SOURCE/DIR_SOURCE) the path is normalized to a leading
// "/" so the generated bootstrap resolves it from the workspace root
// (cwd-independent), not the client process's cwd.
func (funcs goTemplateFuncs) boundModule() generator.BoundModule {
	m := funcs.cfg.ClientConfig.BoundModule
	if m.Kind != generator.ModuleKindGit && m.Path != "" && !strings.HasPrefix(m.Path, "/") {
		m.Path = "/" + m.Path
	}
	return m
}

func (funcs goTemplateFuncs) isSharedCoreObject(ref *introspection.TypeRef) bool {
	if !funcs.cfg.UnifiedClient || ref == nil {
		return false
	}
	for ref.OfType != nil {
		ref = ref.OfType
	}
	typ := funcs.fullSchema.Types.Get(ref.Name)
	return typ != nil && typ.Name != "Query" && typ.Directives.SourceMap() == nil && (typ.Kind == introspection.TypeKindObject || typ.Kind == introspection.TypeKindInterface)
}

func (funcs goTemplateFuncs) isSharedCoreObjectType(typ *introspection.Type) bool {
	return funcs.cfg.UnifiedClient && typ != nil && typ.Directives.SourceMap() == nil
}

func (funcs goTemplateFuncs) isSharedCoreHandle(field introspection.Field) bool {
	return funcs.isSharedCoreObjectType(funcs.fullSchema.Types.Get(funcs.IDHandleType(field)))
}
