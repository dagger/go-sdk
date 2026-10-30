// Package identifiers names identifiers by the engine's identifier naming
// rules, through the engine's own Query.formatIdentifiers.
//
// The engine parses a name into words with a versioned dictionary of acronyms
// and terms (engine/naming in dagger/dagger), both to name a module's
// declarations (from CasingVersion on) and to answer formatIdentifiers (on
// every v1 view). A generator that formats names itself, or with another copy
// of those rules, eventually disagrees with the engine, so generators ask the
// engine instead:
//
//   - Styling: Go identifiers are always the engine's formatting
//     (Connector.Names), whatever the schema's version, so generation needs
//     the engine's session.
//   - Modules: the engine names a module's own declarations itself, and
//     answers for them through ModuleSource.naming (see ModuleNames), on
//     either side of the cut: strcase before it. Generators don't predict
//     them.
//   - Lookups: a generator formats the names it knows up front (a schema's
//     types, fields and arguments) in a single GraphQL query, and keeps them
//     in a map. A name it derives while rendering (FooID, a handle's name),
//     and each of the module's own names, is asked for with a direct call
//     when first looked up, and cached. A name the engine can't parse fails
//     the call, and with it generation.
//   - Floor: generation needs an engine at CasingVersion or later, the first
//     release that serves ModuleSource.naming; the Connector refuses an
//     older one.
//   - Versioning: every request passes the version its Names is for (a
//     schema's, a module's, a runtime's), so names are parsed with that
//     version's dictionary whatever version the session is served at.
//     Released dictionaries never change, so the output is stable.
package identifiers

import (
	"strings"

	"golang.org/x/mod/semver"
)

// CasingVersion is engine.IdentifierCasingVersion in dagger/dagger: the first
// engine release, and API view, that names module declarations with its
// identifier naming rules (and whose core API follows the GraphQL casing
// convention). It's also taken to be the first release serving
// ModuleSource.naming (dagger/dagger#14996), the Go SDK's engine floor.
// Released versions never move it.
const CasingVersion = "v1.0.0-beta.22"

// Casing is a value of the engine's Casing enum: how words are joined.
type Casing string

const (
	Pascal         Casing = "PASCAL"
	Camel          Casing = "CAMEL"
	ScreamingSnake Casing = "SCREAMING_SNAKE"
	Kebab          Casing = "KEBAB"
)

// AcronymStyle is a value of the engine's AcronymStyle enum: how acronyms and
// terms are written where a word starts with a capital.
type AcronymStyle string

const (
	Uppercase   AcronymStyle = "UPPERCASE"
	Capitalized AcronymStyle = "CAPITALIZED"
)

// Format is how formatIdentifiers formats a name.
type Format struct {
	Casing   Casing
	Acronyms AcronymStyle
}

// GoName is the format Go codegen uses: an exported Go identifier, a type,
// method, option struct or option field (CommitSHA, WithGPU, ParentSHAs).
var GoName = Format{Pascal, Uppercase}

// Applies reports whether a version is at or past CasingVersion. The version
// is an engine release (Query.version), a schema's __schemaVersion, or a
// module's engine version. Build metadata is dropped, and a development
// build counts as its release (v1.0.0-dev-<commit> is v1.0.0), as the engine
// maps versions to API views; other prereleases order as semver says, so
// every v1.0.0 beta before CasingVersion is before the cut. An invalid
// version doesn't apply.
func Applies(version string) bool {
	v := release(version)
	return semver.IsValid(v) && semver.Compare(v, CasingVersion) >= 0
}

// release normalizes a version the way the engine maps it to an API view.
func release(version string) string {
	v := strings.TrimSpace(version)
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	v = strings.TrimSuffix(v, semver.Build(v))
	if pre := semver.Prerelease(v); strings.Contains(pre, "dev") {
		v = strings.TrimSuffix(v, pre)
	}
	return v
}
