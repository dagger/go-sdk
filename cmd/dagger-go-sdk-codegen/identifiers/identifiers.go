// Package identifiers names identifiers by the engine's identifier naming
// rules, through the engine's own Query.formatIdentifiers, for code generated
// from schemas at or past CasingVersion.
//
// From CasingVersion on, the engine parses a name into words with a versioned
// dictionary of acronyms and terms (engine/naming in dagger/dagger), both to
// name a module's declarations and to answer formatIdentifiers. A generator
// that formats names itself, or with another copy of those rules, eventually
// disagrees with the engine, so generators ask the engine instead:
//
//   - Gate: Connector.Names returns nil, the legacy rules, unless both the
//     generated schema's version and the engine's release are at or past
//     CasingVersion. Older engines name every module by strcase, and older
//     schemas keep the names they shipped with, so nil means the generator
//     produces exactly what it did before the rules existed. Nothing gates on
//     whether formatIdentifiers is in the schema: engines serve it on every
//     v1 view, including the views whose modules keep the legacy rules. The
//     engine asked is the session's, so a schema read from a file is taken
//     to come from it: an older engine's schemas report v1.0.0, which only
//     its release tells apart from a schema past the cut.
//   - Lookups: a generator formats the names it knows up front (a schema's
//     types, fields and arguments) in a single GraphQL query, one aliased
//     formatIdentifiers field per Format, and keeps them in a map. A name it
//     derives while rendering (FooID, a handle's name) is formatted with a
//     direct call when first looked up, and cached. A name the engine can't
//     parse fails the call, and with it generation.
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
// convention). Released versions never move it.
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

// The formats Go codegen uses. GoName and TypeName are the same format, so
// they share their formatted names.
var (
	// GoName is an exported Go identifier: a type, method, option struct or
	// option field (CommitSHA, WithGPU, ParentSHAs).
	GoName = Format{Pascal, Uppercase}
	// TypeName is the schema name the engine gives a module's object,
	// interface or enum (MyModHTTPClient).
	TypeName = Format{Pascal, Uppercase}
	// FieldName is the schema name the engine gives a module's field,
	// function or argument (baseUrl, httpClient).
	FieldName = Format{Camel, Capitalized}
	// Words is a name's words, lowercase and joined by "-" (http-client):
	// how a generator tells where the engine cuts a name into words.
	Words = Format{Kebab, Uppercase}
)

// Applies reports whether a version is at or past CasingVersion. The version
// is an engine release (Query.version), a schema's __schemaVersion, or a
// module's engine version. Build metadata is dropped, and a development
// build counts as its release (v1.0.0-dev-<commit> is v1.0.0), as the engine
// maps versions to API views; other prereleases order as semver says, so
// every v1.0.0 beta before CasingVersion keeps the legacy rules. An invalid
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
