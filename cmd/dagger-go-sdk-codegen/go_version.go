package main

import (
	"fmt"
	"go/version"
	"os"
	"strings"

	"golang.org/x/mod/modfile"
)

// requiredGoVersion returns the newest requirement from the go and toolchain
// directives and the generator minimum. Keep patch versions: a compiler image
// for the same language version can still be too old to load the module.
func requiredGoVersion(goModPath, minimum string) (string, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("read module go.mod: %w", err)
	}
	file, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return "", fmt.Errorf("parse module go.mod: %w", err)
	}
	if file.Go == nil || !version.IsValid("go"+file.Go.Version) {
		return "", fmt.Errorf("module go.mod has no valid go directive")
	}

	selected := "go" + file.Go.Version
	if minimum != "" {
		minimum = "go" + strings.TrimPrefix(minimum, "go")
		if !version.IsValid(minimum) {
			return "", fmt.Errorf("invalid minimum Go version %q", minimum)
		}
		if version.Compare(minimum, selected) > 0 {
			selected = minimum
		}
	}
	if file.Toolchain != nil && file.Toolchain.Name != "default" {
		toolchain := file.Toolchain.Name
		if !version.IsValid(toolchain) {
			return "", fmt.Errorf("module go.mod has invalid toolchain directive %q", toolchain)
		}
		if version.Compare(toolchain, selected) > 0 {
			selected = toolchain
		}
	}

	return strings.TrimPrefix(selected, "go"), nil
}
