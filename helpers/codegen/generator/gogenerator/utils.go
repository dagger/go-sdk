package gogenerator

import (
	"go/token"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

const daggerImportPath = "dagger.io/dagger"

// defaultClientPackageName names a client whose location yields no usable Go
// identifier. It is what every generated client used to be called.
const defaultClientPackageName = "dagger"

// clientPackageName picks the Go package name a generated client declares.
//
// It is the directory the client was generated into, so two clients generated
// into two directories are two distinct packages a caller can import side by
// side without renaming either ("internal/dagger/engine-dev" -> "enginedev").
// The last element of the client's module path covers the case where the SDK
// did not say where the client lives.
func clientPackageName(clientPath, packageImport string) string {
	candidates := []string{
		path.Base(filepath.ToSlash(clientPath)),
		path.Base(packageImport),
	}
	for _, candidate := range candidates {
		if name := goPackageIdent(candidate); name != "" {
			return name
		}
	}
	return defaultClientPackageName
}

// goPackageIdent reduces one path element to a lowercase Go identifier by
// dropping everything that cannot appear in one ("engine-dev" -> "enginedev").
// It returns "" when nothing usable is left or the result is a keyword, so the
// caller can fall through to the next candidate.
func goPackageIdent(elem string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(elem) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		}
	}
	// An identifier cannot start with a digit, and a package named "_" is not
	// importable.
	name := strings.TrimLeft(b.String(), "0123456789_")
	if name == "" || token.IsKeyword(name) {
		return ""
	}
	return name
}

func isDaggerPkgCustomReplaced(replaces []*modfile.Replace) bool {
	for _, r := range replaces {
		if r.Old.Path == daggerImportPath {
			return true
		}
	}

	return false
}
