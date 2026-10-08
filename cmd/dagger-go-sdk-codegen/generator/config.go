package generator

type Config struct {
	// OutputDir is the path to put the generated code.
	OutputDir string

	// PackageImport is the import path for the generated package inside its Go
	// module.
	PackageImport string

	// UnifiedClient uses shared core bindings and a borrowed lazy session.
	UnifiedClient bool
	PackageName   string

	// ModuleConfig is the specific config to generate a module.
	//
	// Client generation never sets it; it only remains so template helpers
	// shared with upstream (IsModuleCode, ModuleRelPath) keep their shape.
	ModuleConfig *ModuleGeneratorConfig

	// ClientConfig is the specific config to generate standalone client.
	ClientConfig *ClientGeneratorConfig

	// CoreLibrary generates the core bindings of dagger.io/dagger: the
	// dagger.io/dagger/core package, and the deprecated dagger.io/dagger/dag
	// package that forwards to it.
	CoreLibrary bool
}

// Specific configuration for module generation.
type ModuleGeneratorConfig struct {
	// Name of the module to generate code for.
	ModuleName string

	// ModuleSourcePath is the subpath in OutputDir where the module source subpath is located.
	ModuleSourcePath string

	// ModuleParentPath is the path from the module source subpath to the context directory
	ModuleParentPath string

	// Whether generation is part of module initialization.
	IsInit bool

	// LibVersion is the dagger.io/dagger version used by the generated module.
	LibVersion string
}

// ModuleSourceDependency describes one dependency in a module schema.
// Module code generation keeps this type because its hardened templates also
// support dependency-aware module clients.
type ModuleSourceDependency struct {
	Kind   string
	Name   string `json:"moduleOriginalName"`
	Pin    string
	Source string `json:"asString"`
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
	// BoundModule is the single module the generated client serves; it drives
	// the generated serveBoundModule bootstrap.
	BoundModule BoundModule

	// ModuleDependencies is retained for module-client template compatibility.
	// Standalone clients use BoundModule instead.
	ModuleDependencies []ModuleSourceDependency
}
