package gogenerator

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml"
	"github.com/psanford/memfs"
)

// vcsGeneratedPaths are marked linguist-generated, as the engine marks them
// for the native Go SDK. internal/telemetry is no longer generated but the
// engine still lists it; keeping the list identical keeps regeneration of an
// engine-generated module free of .gitattributes churn.
var vcsGeneratedPaths = []string{
	"dagger.gen.go",
	"internal/dagger/**",
	"internal/telemetry/**",
}

// vcsIgnoredPaths is what survives of the engine's ignore list for a
// dagger-module.toml module: generated files are committed, so only .env is
// left to ignore.
var vcsIgnoredPaths = []string{".env"}

const moduleConfigFilename = "dagger-module.toml"

// writeVCSFiles appends the generated-path and ignore entries to the source
// directory's .gitattributes and .gitignore the way the engine's runCodegen
// does: an entry whose text already occurs anywhere in the file is skipped.
func writeVCSFiles(mfs *memfs.FS, sourceDir string, gitignore bool) error {
	attrs, err := readIfExists(filepath.Join(sourceDir, ".gitattributes"))
	if err != nil {
		return err
	}
	if err := mfs.WriteFile(".gitattributes", appendVCSEntries(attrs, vcsGeneratedPaths, "/%s linguist-generated\n"), 0o600); err != nil {
		return err
	}
	if !gitignore {
		return nil
	}
	ignore, err := readIfExists(filepath.Join(sourceDir, ".gitignore"))
	if err != nil {
		return err
	}
	return mfs.WriteFile(".gitignore", appendVCSEntries(ignore, vcsIgnoredPaths, "/%s\n"), 0o600)
}

func appendVCSEntries(existing []byte, entries []string, format string) []byte {
	out := existing
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	for _, entry := range entries {
		if bytes.Contains(out, []byte(entry)) {
			// already has some config for the file
			continue
		}
		out = fmt.Appendf(out, format, strings.TrimPrefix(entry, "/"))
	}
	return out
}

func readIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// automaticGitignore reports whether the module's dagger-module.toml leaves
// `[codegen] automaticGitignore` at its default (true). rootPath is the
// output-relative module root, which may be above the source directory
// (`source = "src"`); searching upwards instead would let an ancestor
// module's config decide for a nested one.
func automaticGitignore(outputDir, rootPath string) (bool, error) {
	configPath := filepath.Join(outputDir, rootPath, moduleConfigFilename)
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var cfg struct {
		Codegen *struct {
			AutomaticGitignore *bool `toml:"automaticGitignore"`
		} `toml:"codegen"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return false, fmt.Errorf("parse %s: %w", configPath, err)
	}
	if cfg.Codegen == nil || cfg.Codegen.AutomaticGitignore == nil {
		return true, nil
	}
	return *cfg.Codegen.AutomaticGitignore, nil
}
