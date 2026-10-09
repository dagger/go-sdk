package gogenerator

import (
	"go/token"
	"path"
	"path/filepath"
	"strings"
)

const defaultClientPackageName = "dagger"

// clientPackageName names a standalone client after its output directory.
// The import path is a fallback for callers that do not provide one.
func clientPackageName(outputDir, packageImport string) string {
	candidates := []string{
		path.Base(filepath.ToSlash(outputDir)),
		path.Base(packageImport),
	}
	for _, candidate := range candidates {
		if name := goPackageIdent(candidate); name != "" {
			return name
		}
	}
	return defaultClientPackageName
}

// goPackageIdent reduces one path element to a conventional Go package name.
func goPackageIdent(elem string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(elem) {
		if r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	name := strings.TrimLeft(b.String(), "0123456789_")
	if name == "" || token.IsKeyword(name) {
		return ""
	}
	return name
}
