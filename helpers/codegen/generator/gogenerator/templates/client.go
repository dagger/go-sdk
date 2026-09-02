package templates

import (
	"strings"

	"codegen/generator"
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
