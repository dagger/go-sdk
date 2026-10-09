package gogenerator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/psanford/memfs"
	"golang.org/x/mod/modfile"
)

// Generate stages the complete operation, including package loading. Only a
// successfully generated, collision-checked tree is copied back to the author.
func Generate(ctx context.Context, cfg GenerateConfig) error {
	root, err := filepath.Abs(cfg.ModuleRoot)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); errors.Is(err, os.ErrNotExist) {
		for parent := filepath.Dir(root); ; parent = filepath.Dir(parent) {
			if _, err := os.Stat(filepath.Join(parent, "go.mod")); err == nil {
				return fmt.Errorf("Dang entrypoints need a go.mod in the module directory; keep the Go runtime for modules using a parent go.mod")
			}
			if filepath.Dir(parent) == parent {
				break
			}
		}
	}
	if cfg.UnifiedClient && cfg.DaggerVersion == "" {
		data, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil {
			return fmt.Errorf("unified module generation needs a clientVersion containing the core split, or a local SDK replacement")
		}
		mod, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			return err
		}
		replaced := false
		for _, replacement := range mod.Replace {
			if replacement.Old.Path == "dagger.io/dagger" {
				replaced = true
			}
		}
		if !replaced {
			return fmt.Errorf("unified module generation needs a clientVersion containing the core split, or a local SDK replacement")
		}
	}
	old, err := generator.ReadOwnership(root)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "dagger-module-generate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.CopyFS(stage, os.DirFS(root)); err != nil {
		return fmt.Errorf("stage module generation: %w", err)
	}
	// Remove unchanged prior outputs from the staged copy. Current generation
	// recreates the paths it still owns; renamed dispatchers and collection
	// runtime sources therefore cannot survive as stale artifacts.
	if err := generator.PruneOwnedClient(ctx, stage); err != nil {
		return err
	}

	stagedCfg := cfg
	stagedCfg.ModuleRoot = stage
	stagedCfg.RemovedPath = filepath.Join(stage, "removed-paths.txt")
	if err := generate(ctx, stagedCfg); err != nil {
		return err
	}

	removed, err := applyGeneratedTree(ctx, root, stage, old, stagedCfg.RemovedPath)
	if err != nil {
		return err
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(stage, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(root, name), data); err != nil {
			return err
		}
	}
	if cfg.RemovedPath != "" {
		var contents string
		if len(removed) > 0 {
			contents = strings.Join(removed, "\n") + "\n"
		}
		if err := os.WriteFile(cfg.RemovedPath, []byte(contents), 0o644); err != nil {
			return fmt.Errorf("write removed file list: %w", err)
		}
	}
	return nil
}

func applyGeneratedTree(ctx context.Context, root, stage string, old generator.Ownership, legacyRemovedPath string) ([]string, error) {
	overlay := memfs.New()
	err := fs.WalkDir(os.DirFS(stage), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == ".git" || path == "internal/dagger/clients" {
				return fs.SkipDir
			}
			return nil
		}
		ownedPath := path == "dagger.gen.go" ||
			path == "dagger.dispatch.gen.go" ||
			strings.HasPrefix(path, "internal/dagger/") ||
			strings.HasPrefix(path, "cmd/") && strings.Contains(path, "-dispatch/")
		if !ownedPath {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		if !generatedMarker.Match(data) {
			return nil
		}
		if hash, tracked := old.Files[path]; tracked {
			original, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			sum := sha256.Sum256(original)
			// If generation left a user-edited tracked file untouched, preserve
			// it and release ownership instead of adopting the edit as generated.
			if hex.EncodeToString(sum[:]) != hash && bytes.Equal(data, original) {
				return nil
			}
		}
		if err := overlay.MkdirAll(filepath.ToSlash(filepath.Dir(path)), 0o755); err != nil {
			return err
		}
		return overlay.WriteFile(path, data, 0o644)
	})
	if err != nil {
		return nil, err
	}

	removed, err := generator.WriteOwnedOverlay(ctx, overlay, root)
	if err != nil {
		return nil, err
	}
	// Migrate stale, untracked outputs from the previous header-only scheme.
	legacyRemoved, err := os.ReadFile(legacyRemovedPath)
	if err != nil {
		return nil, err
	}
	for _, path := range strings.Split(string(legacyRemoved), "\n") {
		if path == "" {
			continue
		}
		if _, tracked := old.Files[path]; tracked {
			continue
		}
		if !fs.ValidPath(path) {
			return nil, fmt.Errorf("invalid removed path %q", path)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if generatedMarker.Match(data) {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
				return nil, err
			}
			removed = append(removed, path)
		}
	}
	sort.Strings(removed)
	return removed, nil
}
