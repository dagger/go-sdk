package templates

import (
	"path/filepath"
	"strings"

	"codegen/generator"
)

func (funcs goTemplateFuncs) isModuleCode() bool {
	return funcs.cfg.ModuleConfig != nil && funcs.cfg.ModuleConfig.ModuleName != ""
}

func (funcs goTemplateFuncs) isStandaloneClient() bool {
	return funcs.cfg.ClientConfig != nil
}

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

func (funcs goTemplateFuncs) moduleRelPath(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}

	moduleParentPath := ""
	if funcs.cfg.ModuleConfig != nil {
		moduleParentPath = funcs.cfg.ModuleConfig.ModuleParentPath
	}

	return filepath.Join(
		// path to the root of this module (since we're probably in internal/dagger/)
		"../..",
		// path from the module root to the context directory
		moduleParentPath,
		// path from the context directory to the desired path
		path,
	)
}
