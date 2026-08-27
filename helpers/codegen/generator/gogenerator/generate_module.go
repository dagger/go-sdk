package gogenerator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/token"
	"go/version"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/iancoleman/strcase"
	"github.com/psanford/memfs"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"golang.org/x/tools/go/packages"

	"codegen/generator"
	"codegen/generator/gogenerator/templates"
	"codegen/introspection"
)

// selfTypesMinVersion is the first schema view whose bindings can carry the
// module's own types. Older views alias every schema type into the main
// package (see _dagger.gen.go/module.go.tmpl), where the module's types would
// collide with the user's own declarations.
const selfTypesMinVersion = "v0.12.0"

func supportsSelfTypes(schemaVersion string) bool {
	return semver.Compare(schemaVersion, selfTypesMinVersion) >= 0
}

// GenerateModuleTypes bootstraps the module to a loadable state and, once it
// is, emits the module's own types as introspection JSON for the caller to
// merge into the dependency schema before calling GenerateModule.
//
// It shares the bootstrap loop with GenerateModule: while the returned state
// carries NeedRegenerate the JSON is nil and the caller must apply the state
// (overlay and post-commands) and call again. A nil JSON on a final state
// means the schema view predates self types and there is nothing to merge.
func (g *GoGenerator) GenerateModuleTypes(ctx context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, []byte, error) {
	if g.Config.ModuleConfig == nil {
		return nil, nil, fmt.Errorf("GenerateModuleTypes called but module config is missing")
	}
	moduleName := g.Config.ModuleConfig.ModuleName

	// The merge is keyed on the module name: a dependency installed under the
	// module's own name would make it a silent no-op and the module's types
	// would never enter the schema. Refuse it with an actionable error.
	if slices.Contains(schema.DependencyNames(), moduleName) {
		return nil, nil, fmt.Errorf(
			"a dependency is installed under the module's own name %q; "+
				"self-calls need distinct names — reinstall the dependency under "+
				"another name (dagger install --name)", moduleName)
	}

	_, _, outDir, genSt, partial, err := g.bootstrapModule(ctx, schema, schemaVersion)
	if err != nil {
		return nil, nil, err
	}
	if partial {
		genSt.NeedRegenerate = true
		return genSt, nil, nil
	}
	if !supportsSelfTypes(schemaVersion) {
		return genSt, nil, nil
	}

	pkg, fset, err := g.loadModulePackage(ctx, outDir)
	if err != nil {
		return nil, nil, err
	}

	emitter := templates.NewModuleIntrospectionEmitter(ctx, schema, schemaVersion, g.Config, pkg, fset)
	typesJSON, err := emitter.ModuleIntrospectionJSON(moduleName)
	if err != nil {
		return nil, nil, fmt.Errorf("emit module types: %w", err)
	}
	return genSt, typesJSON, nil
}

// GenerateModule generates a Go module's bindings from a schema that already
// carries the module's own types. Unlike the engine's cmd/codegen it does not
// merge them itself: the SDK's Dang layer merges the JSON GenerateModuleTypes
// emitted through the engine's schema().merge() and hands the result in. The
// rest mirrors the engine: bootstrap go.mod and a base dagger.gen.go for a
// fresh module (requesting another pass), load the module package, render
// every binding file, and report the per-dependency bindings that went stale.
func (g *GoGenerator) GenerateModule(ctx context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	mfs, pkgInfo, outDir, genSt, partial, err := g.bootstrapModule(ctx, schema, schemaVersion)
	if err != nil {
		return nil, err
	}
	if partial {
		genSt.NeedRegenerate = true
		return genSt, nil
	}

	pkg, fset, err := g.loadModulePackage(ctx, outDir)
	if err != nil {
		return nil, err
	}

	// respect existing package name
	pkgInfo.PackageName = pkg.Name

	if err := generateCode(ctx, g.Config, schema, schemaVersion, mfs, pkgInfo, &moduleGenCtx{pkg: pkg, fset: fset, pass: 1}); err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	staleBindings, err := findStaleDependencyBindings(
		g.Config.OutputDir,
		filepath.Join(outDir, internalDaggerDir),
		genSt.Overlay,
	)
	if err != nil {
		return nil, fmt.Errorf("find stale dependency bindings: %w", err)
	}
	genSt.RemovePaths = append(genSt.RemovePaths, staleBindings...)

	sourceDir := filepath.Join(g.Config.OutputDir, outDir)
	gitignore, err := automaticGitignore(g.Config.OutputDir, g.Config.ModuleConfig.ModuleRootPath)
	if err != nil {
		return nil, err
	}
	if err := writeVCSFiles(mfs, sourceDir, gitignore); err != nil {
		return nil, fmt.Errorf("write vcs files: %w", err)
	}

	return genSt, nil
}

// loadModulePackage loads the module package at outDir.
func (g *GoGenerator) loadModulePackage(ctx context.Context, outDir string) (*packages.Package, *token.FileSet, error) {
	dir := filepath.Join(g.Config.OutputDir, outDir)
	pkg, fset, err := loadPackage(ctx, dir, false)
	if err != nil {
		return nil, nil, fmt.Errorf("load package %q: %w", outDir, err)
	}
	return pkg, fset, nil
}

// bootstrapModule writes go.mod (and, for a module without a dagger.gen.go
// yet, base bindings rendered from the deps schema) into the overlay. When
// partial is true another pass is needed before the module can be loaded:
// the caller applies the overlay and the post-commands, then calls again.
// The returned memfs is rooted at the module source directory.
func (g *GoGenerator) bootstrapModule(ctx context.Context, schema *introspection.Schema, schemaVersion string) (_ *memfs.FS, _ *PackageInfo, outDir string, _ *generator.GeneratedState, partial bool, _ error) {
	if g.Config.ModuleConfig == nil {
		return nil, nil, "", nil, false, fmt.Errorf("GenerateModule called but module config is missing")
	}
	moduleConfig := g.Config.ModuleConfig

	generator.SetSchema(schema)

	outDir = filepath.Clean(moduleConfig.ModuleSourcePath)

	mfs := memfs.New()
	genSt := &generator.GeneratedState{Overlay: mfs}

	sdkMod, err := g.libraryGoMod(ctx)
	if err != nil {
		return nil, nil, "", nil, false, err
	}

	pkgInfo, needsRegen, err := g.bootstrapMod(mfs, genSt, sdkMod)
	if err != nil {
		return nil, nil, "", nil, false, fmt.Errorf("bootstrap package: %w", err)
	}
	partial = needsRegen

	// Initializing a module is initModule's job, so generate only runs on
	// initialized modules. The check belongs here rather than at the package
	// load, which the bootstrap's own dagger.gen.go would already satisfy.
	goFiles, err := filepath.Glob(filepath.Join(g.Config.OutputDir, outDir, "*.go"))
	if err != nil {
		return nil, nil, "", nil, false, fmt.Errorf("glob go files: %w", err)
	}
	if len(goFiles) == 0 {
		return nil, nil, "", nil, false, fmt.Errorf("module source %q has no Go files; run `dagger module init` first", outDir)
	}

	if outDir != "." {
		if err := mfs.MkdirAll(outDir, 0o700); err != nil {
			return nil, nil, "", nil, false, err
		}
		sub, err := mfs.Sub(outDir)
		if err != nil {
			return nil, nil, "", nil, false, err
		}
		mfs = sub.(*memfs.FS)
	}

	genFile := filepath.Join(g.Config.OutputDir, outDir, ClientGenFile)
	if _, err := os.Stat(genFile); err != nil {
		// assume package main, default for modules
		pkgInfo.PackageName = "main"

		// render base bindings from the deps schema so the module's own source
		// can type-check against internal/dagger on the next pass
		if err := generateCode(ctx, g.Config, schema, schemaVersion, mfs, pkgInfo, &moduleGenCtx{pass: 0}); err != nil {
			return nil, nil, "", nil, false, fmt.Errorf("generate code: %w", err)
		}
		partial = true
	}

	return mfs, pkgInfo, outDir, genSt, partial, nil
}

func (g *GoGenerator) bootstrapMod(mfs *memfs.FS, genSt *generator.GeneratedState, sdkMod *modfile.File) (*PackageInfo, bool, error) {
	moduleConfig := g.Config.ModuleConfig

	// Resolved up front so a toolchain whose version does not parse (a devel
	// build) fails here rather than silently skewing the version guard below.
	langVersion, err := goLanguageVersion(runtime.Version())
	if err != nil {
		return nil, false, err
	}

	var needsRegen bool

	var daggerModPath string
	var goMod *modfile.File

	modname := fmt.Sprintf("dagger/%s", strcase.ToKebab(moduleConfig.ModuleName))
	// check for a go.mod already for the dagger module
	if content, readErr := os.ReadFile(filepath.Join(g.Config.OutputDir, moduleConfig.ModuleSourcePath, "go.mod")); readErr == nil {
		daggerModPath = moduleConfig.ModuleSourcePath

		goMod, err = modfile.ParseLax("go.mod", content, nil)
		if err != nil {
			return nil, false, fmt.Errorf("parse go.mod: %w", err)
		}
	}

	// could not find a go.mod, so we can init a basic one
	if goMod == nil {
		daggerModPath = moduleConfig.ModuleSourcePath
		goMod = new(modfile.File)

		goMod.AddModuleStmt(modname)
		goMod.AddGoStmt(langVersion)

		needsRegen = true
	}

	// sanity check the parsed go version
	//
	// if this fails, then the go.mod version is too high! and in that case, we
	// won't be able to load the resulting package
	if goMod.Go == nil {
		return nil, false, fmt.Errorf("go.mod has no go directive")
	}
	// A devel toolchain has no version go/version can compare against, so the
	// guard falls back to the language version derived above.
	toolchain := runtime.Version()
	if !version.IsValid(toolchain) {
		toolchain = "go" + langVersion
	}
	if version.Compare("go"+goMod.Go.Version, toolchain) > 0 {
		return nil, false, fmt.Errorf("existing go.mod has unsupported version %v (highest supported version is %v); the pinned dagger.io/dagger (goSDKLibVersion) and this codegen's Go toolchain move together, so raising one needs the other bumped too", goMod.Go.Version, goVersion)
	}

	if err := g.syncModReplaceAndTidy(goMod, genSt, daggerModPath, sdkMod); err != nil {
		return nil, false, err
	}

	modBody, err := goMod.Format()
	if err != nil {
		return nil, false, fmt.Errorf("format go.mod: %w", err)
	}

	if err := mfs.MkdirAll(daggerModPath, 0o700); err != nil {
		return nil, false, err
	}
	if err := mfs.WriteFile(filepath.Join(daggerModPath, "go.mod"), modBody, 0o600); err != nil {
		return nil, false, err
	}

	packageImport, err := filepath.Rel(daggerModPath, moduleConfig.ModuleSourcePath)
	if err != nil {
		return nil, false, err
	}
	return &PackageInfo{
		// PackageName is unknown until we load the package
		PackageImport: path.Join(goMod.Module.Mod.Path, packageImport),

		DaggerPkgReplaced: isDaggerPkgCustomReplaced(goMod.Replace),
	}, needsRegen, nil
}

// syncModReplaceAndTidy seeds the module's go.mod from the library's (minimum
// versions and, for paths the module already requires, replace directives)
// and queues the post-commands that pin dagger.io/dagger, tidy, and enrol the
// module in an enclosing go.work. sdkMod may be nil, in which case nothing is
// seeded.
func (g *GoGenerator) syncModReplaceAndTidy(mod *modfile.File, genSt *generator.GeneratedState, modPath string, sdkMod *modfile.File) error {
	modDir := filepath.Join(g.Config.OutputDir, modPath)

	// if there is a go.work, we need to also set overrides there, otherwise
	// modules will have individually conflicting replace directives
	goWork, err := goWorkPath(modDir)
	if err != nil {
		return fmt.Errorf("find go.work: %w", err)
	}

	modRequires := make(map[string]*modfile.Require)
	for _, req := range mod.Require {
		modRequires[req.Mod.Path] = req
	}
	if sdkMod != nil {
		// use the library's go.mod as basis for pinning versions
		for _, minReq := range sdkMod.Require {
			// check if mod already at least this version
			if currentReq, ok := modRequires[minReq.Mod.Path]; ok {
				if semver.Compare(currentReq.Mod.Version, minReq.Mod.Version) >= 0 {
					continue
				}
			}
			modRequires[minReq.Mod.Path] = minReq
			mod.AddNewRequire(minReq.Mod.Path, minReq.Mod.Version, minReq.Indirect)
		}

		// preserve any replace directives in the library's go.mod (e.g. pre-1.0 packages)
		for _, minReq := range sdkMod.Replace {
			if _, ok := modRequires[minReq.New.Path]; !ok {
				// ignore anything that's library-only
				continue
			}
			genSt.PostCommands = append(genSt.PostCommands,
				exec.Command("go", "mod", "edit", "-replace", minReq.Old.Path+"="+minReq.New.Path+"@"+minReq.New.Version))
			if goWork != "" {
				genSt.PostCommands = append(genSt.PostCommands,
					exec.Command("go", "work", "edit", "-replace", minReq.Old.Path+"="+minReq.New.Path+"@"+minReq.New.Version))
			}
		}
	}

	// Check if the module go.mod replaces the dagger.io/dagger library with a custom path.
	// If so, we keep it as is.
	// Otherwise, we install the given dagger.io/dagger package version.
	if libVersion := g.Config.ModuleConfig.LibVersion; libVersion != "" && !isDaggerPkgCustomReplaced(mod.Replace) {
		genSt.PostCommands = append(genSt.PostCommands,
			// Do not pass -u here: LibVersion pins dagger.io/dagger, while -u also
			// asks Go to upgrade transitive dependencies during generation.
			exec.Command("go", "get", daggerImportPath+"@"+libVersion))
	}

	genSt.PostCommands = append(genSt.PostCommands,
		// run 'go mod tidy' after generating to fix and prune dependencies
		//
		// NOTE: this has to happen before 'go work use' to synchronize Go version
		// bumps
		exec.Command("go", "mod", "tidy"),
	)

	if goWork != "" {
		// run "go work use ." after generating if we had a go.work at the root
		genSt.PostCommands = append(genSt.PostCommands, exec.Command("go", "work", "use", "."))
	}

	return nil
}

// libraryGoMod resolves the pinned dagger.io/dagger's go.mod through the
// module proxy, the same file the engine embeds. `go list -m` fetches only
// the .info and .mod entries, and runs outside any module so the pin is
// resolved on its own rather than against the module's graph. The result is
// cached for the bootstrap loop.
func (g *GoGenerator) libraryGoMod(ctx context.Context) (*modfile.File, error) {
	if g.libraryMod != nil {
		return g.libraryMod, nil
	}
	libVersion := g.Config.ModuleConfig.LibVersion
	if libVersion == "" {
		return nil, nil
	}

	dir, err := os.MkdirTemp("", "codegen-library-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", daggerImportPath+"@"+libVersion)
	cmd.Dir = dir
	// An inherited -mod=vendor or an enclosing go.work would resolve the pin
	// against the caller's build list instead of the proxy, or fail outright.
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("resolve %s@%s: %w", daggerImportPath, libVersion, err)
	}
	var info struct {
		GoMod string
	}
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parse go list -m output: %w", err)
	}
	data, err := os.ReadFile(info.GoMod)
	if err != nil {
		return nil, fmt.Errorf("read %s go.mod: %w", daggerImportPath, err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %s go.mod: %w", daggerImportPath, err)
	}
	g.libraryMod = mod
	return mod, nil
}

// goWorkPath returns the go.work in effect in dir, or "" when there is none:
// `go env GOWORK` spells that either as an empty value or as "off".
func goWorkPath(dir string) (string, error) {
	goWork, err := goEnv(dir, "GOWORK")
	if err != nil {
		return "", err
	}
	if goWork == "off" {
		return "", nil
	}
	return goWork, nil
}

func goEnv(dir string, env string) (string, error) {
	buf := new(bytes.Buffer)
	findGoWork := exec.Command("go", "env", env)
	findGoWork.Dir = dir
	findGoWork.Stdout = buf
	findGoWork.Stderr = os.Stderr
	if err := findGoWork.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}
