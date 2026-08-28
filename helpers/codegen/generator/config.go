package generator

type Config struct {
	// OutputDir is the path to put the generated code.
	// This allows generating extra files aside the client bindings
	// like go.mod.
	OutputDir string

	// IntrospectionJSON is an optional pre-computed introspection json string.
	IntrospectionJSON string

	// ModuleConfig is the specific config to generate a module.
	//
	// Client generation never sets it; it only remains so template helpers
	// shared with upstream (IsModuleCode, ModuleRelPath) keep their shape.
	ModuleConfig *ModuleGeneratorConfig

	// ClientConfig is the specific config to generate standalone client.
	ClientConfig *ClientGeneratorConfig
}

// Specific configuration for module generation.
type ModuleGeneratorConfig struct {
	// Name of the module to generate code for.
	ModuleName string

	// ModuleRootPath is the subpath in OutputDir of the module root — the
	// directory holding dagger-module.toml, at or above ModuleSourcePath.
	ModuleRootPath string

	// ModuleSourcePath is the subpath in OutputDir where the module source subpath is located.
	ModuleSourcePath string

	// ModuleParentPath is the path from the module source subpath to the context directory
	ModuleParentPath string

	// LibVersion pins dagger.io/dagger in the generated module's go.mod
	// (`go get dagger.io/dagger@<LibVersion>`).
	LibVersion string
}

// Module-source kinds a generated client can bind to. A local module
// (LOCAL_SOURCE, or DIR_SOURCE — how a workspace-local module resolves in
// practice) is served by its workspace-relative path; a GIT_SOURCE module is
// served from its canonical ref + pin.
const (
	ModuleKindGit   = "GIT_SOURCE"
	ModuleKindLocal = "LOCAL_SOURCE"
	ModuleKindDir   = "DIR_SOURCE"
)

// BoundModule identifies the single module a generated client serves. The
// generated serveBoundModule bootstrap uses Kind to decide how to load it at
// runtime: a local module (LOCAL_SOURCE/DIR_SOURCE) is resolved against the
// workspace by its workspace-root-relative Path
// (dag.CurrentWorkspace().ModuleSource(Path)); a git module (GIT_SOURCE) is
// served from its canonical Ref + Pin, which resolve from anywhere.
type BoundModule struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Ref  string `json:"ref"`
	Pin  string `json:"pin"`
}

// Specific configuration for client generation.
type ClientGeneratorConfig struct {
	// The name of the module to generate for.
	ModuleName string

	// BoundModule is the single module the generated client serves; it drives
	// the generated serveBoundModule bootstrap.
	BoundModule BoundModule

	// The engine version from dagger.json, used to pin the dagger.io/dagger dependency.
	// This is only populated when generating from a module source (not in tests).
	EngineVersion string
}
