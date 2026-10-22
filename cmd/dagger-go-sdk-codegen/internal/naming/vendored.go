package naming

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// This package is a verbatim copy of github.com/dagger/dagger's engine/naming
// (every file but this one and vendored_test.go); see README.md for where it
// comes from and how to update it.
//
// go-sdk's codegen runs without an engine connection, so it can't ask the
// engine's Query.formatIdentifiers to format names, and the module's own
// declarations only exist after its Go package has been analyzed. It applies
// the engine's rules itself instead, and this copy must be the engine's.

// VendoredCommit is the github.com/dagger/dagger commit this copy was taken
// from.
const VendoredCommit = "3bae26f41fbfd1ceb7f65ea515da3f72939e76a2"

// KnownVersion is the newest engine version whose naming rules this copy is
// known to have: dictionaries are added by engine version (see releases), and
// a newer engine may have one this copy lacks.
const KnownVersion = "v1.0.0"

// CheckKnownVersion refuses a schema version newer than KnownVersion (by base
// version, as DictionaryFor selects dictionaries), whose naming rules this
// copy may not have: formatting with stale rules would generate identifiers
// that don't match the engine's, and code that doesn't build. An empty
// version is accepted, like DictionaryFor's latest.
func CheckKnownVersion(schemaVersion string) error {
	if schemaVersion == "" {
		return nil
	}
	v := schemaVersion
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return nil
	}
	if semver.Compare(baseVersion(v), KnownVersion) > 0 {
		return fmt.Errorf("schema version %s is newer than the identifier naming rules this Go SDK knows (%s): update the Go SDK, or declare an engine version up to %s", schemaVersion, KnownVersion, KnownVersion)
	}
	return nil
}
