// Package library pins dagger.io/dagger, the core library of generated Go
// code, to the Dagger release that code targets, and checks the pin after
// go mod tidy.
package library

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

const Path = "dagger.io/dagger"

// Replaced reports whether a replace directive of go.mod applies to the
// library at version: one without a version, or one for that version. A
// replace for another version does not reach the build, so it grants no
// development exception.
func Replaced(goModPath, version string) (bool, error) {
	mod, _, err := parse(goModPath)
	if err != nil {
		return false, err
	}
	for _, replace := range mod.Replace {
		if replace.Old.Path == Path && (replace.Old.Version == "" || replace.Old.Version == version) {
			return true, nil
		}
	}
	return false, nil
}

// Problem reports why the module proxy cannot resolve the library at version,
// or an empty string when it can. A development engine reports an unreleased
// version, which has no library yet; a failure to reach the proxy reads the
// same way.
func Problem(dir, version string) string {
	cmd := exec.Command("go", "list", "-m", "-e", "-f", "{{with .Error}}{{.Err}}{{end}}", Path+"@"+version)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return strings.TrimSpace(err.Error() + ": " + stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// CheckResolvable refuses a targeted release whose library the proxy cannot
// resolve. subject names the module or scope.
func CheckResolvable(dir, subject, version string) error {
	problem := Problem(dir, version)
	if problem == "" {
		return nil
	}
	return fmt.Errorf("%s targets Dagger %s, and its Go library %s %s cannot be resolved (%s): set targetVersion to a release whose library is published in the scope's settings in dagger.toml, or replace %s in go.mod for development", subject, version, Path, version, problem, Path)
}

// ReplaceNote is the warning that a replaced library withdraws what the
// targeted release guarantees.
func ReplaceNote(subject, version string) string {
	return fmt.Sprintf("warning: %s targets Dagger %s, and go.mod replaces %s: the generated code uses the replacement's API, which that release does not guarantee; this is a development exception, so remove the replace directive before publishing", subject, version, Path)
}

// Pin writes the library requirement of go.mod: exactly version when exact,
// the higher of version and the existing requirement otherwise, skipping a
// version the proxy cannot resolve. A replace directive that applies keeps
// go.mod as the author wrote it.
func Pin(goModPath, version string, exact bool) error {
	if version == "" {
		return nil
	}
	replaced, err := Replaced(goModPath, version)
	if err != nil || replaced {
		return err
	}
	mod, data, err := parse(goModPath)
	if err != nil {
		return err
	}
	for _, require := range mod.Require {
		if require.Mod.Path != Path {
			continue
		}
		if exact && require.Mod.Version == version || !exact && semver.Compare(require.Mod.Version, version) >= 0 {
			return nil
		}
	}
	if !exact && Problem(filepath.Dir(goModPath), version) != "" {
		return nil
	}
	if err := mod.AddRequire(Path, version); err != nil {
		return err
	}
	body, err := mod.Format()
	if err != nil {
		return err
	}
	if bytes.Equal(body, data) {
		return nil
	}
	if err := os.WriteFile(goModPath, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", goModPath, err)
	}
	return nil
}

// Only a replacement in the final build list grants a development exception:
// tidy can select a version that a target-specific replacement does not cover.
func CheckSelection(ctx context.Context, root, subject, target string) (bool, error) {
	data, err := goOutput(ctx, root, "list", "-m", "-json", Path)
	if err != nil {
		return false, err
	}
	var selected struct {
		Version string
		Replace *struct{}
	}
	if err := json.Unmarshal([]byte(data), &selected); err != nil {
		return false, err
	}
	if selected.Replace != nil {
		return true, nil
	}
	if selected.Version == target {
		return false, nil
	}
	graph, err := goOutput(ctx, root, "mod", "graph")
	if err != nil {
		return false, err
	}
	var raisers, paths []string
	for _, edge := range strings.Split(graph, "\n") {
		from, to, ok := strings.Cut(edge, " ")
		path, version, versioned := strings.Cut(from, "@")
		if !ok || !versioned || to != Path+"@"+selected.Version {
			continue
		}
		raisers = append(raisers, path+" "+version)
		paths = append(paths, path)
	}
	cause := "go.mod selects " + Path + " " + selected.Version
	fix := "remove what selects it from go.mod"
	switch len(raisers) {
	case 0:
	case 1:
		cause = raisers[0] + " requires " + Path + " " + selected.Version
		fix = "require a version of " + paths[0] + " that needs " + Path + " " + target + " or earlier"
	default:
		cause = joinAnd(raisers) + " require " + Path + " " + selected.Version
		fix = "require versions of " + joinAnd(paths) + " that need " + Path + " " + target + " or earlier"
	}
	return false, fmt.Errorf("%s targets Dagger %s, and %s: set targetVersion to %s or later in the scope's settings in dagger.toml, or %s", subject, target, cause, selected.Version, fix)
}

func parse(goModPath string) (*modfile.File, []byte, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return nil, nil, err
	}
	mod, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return nil, nil, err
	}
	return mod, data, nil
}

func goOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func joinAnd(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
