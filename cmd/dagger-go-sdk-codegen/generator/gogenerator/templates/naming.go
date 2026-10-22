package templates

import (
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/internal/naming"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// Go identifiers are schema names formatted like the engine's
// Query.formatIdentifiers formats them, when the schema being generated has
// that field (engine API views v1.0.0-beta.21 and later), loaded onto the
// schema in NameFormats before rendering (see LoadNames). When a name has no
// format (an older schema, or a name that isn't one of the schema's names),
// the legacy conversions apply: golint's initialisms for names, strcase for
// enum values.
//
// Only Go identifiers go through here. Strings sent over the wire (selected
// fields, argument names, enum values, GraphQL type names) always use the
// schema name as is.

var (
	// pascalNames: types, methods, option struct fields and enum values.
	pascalNames = introspection.NameFormat{Casing: naming.Pascal, Acronyms: naming.Uppercase}
	// camelNames: parameters.
	camelNames = introspection.NameFormat{Casing: naming.Camel, Acronyms: naming.Uppercase}
)

// NameFormats are the formats Go codegen names identifiers in.
var NameFormats = []introspection.NameFormat{pascalNames, camelNames}

// LoadNames formats the names of a schema for Go identifiers, with the rules
// of its version, before it is rendered. A schema without
// Query.formatIdentifiers keeps the legacy names.
func LoadNames(schema *introspection.Schema, schemaVersion string) error {
	return schema.LoadFormattedNames(schemaVersion, NameFormats...)
}

// engineName returns a schema name formatted in f, if the schema being
// generated has formatted names.
func engineName(name string, f introspection.NameFormat) (string, bool) {
	return generator.GetSchema().FormattedName(name, f)
}

// formatName formats a GraphQL name (an object, field, or an argument
// becoming an option struct field) into an exported Go identifier: PASCAL
// with uppercase acronyms, e.g. `parentShas` -> `ParentSHAs`. Without a
// formatted name, it falls back to legacyFormatName.
func formatName(s string) string {
	if name, ok := engineName(s, pascalNames); ok {
		return name
	}
	return legacyFormatName(s)
}

// legacyFormatName is how names were formatted before the engine's naming
// rules: capitalize the first letter, then apply golint's initialisms.
// Example: `fooId` -> `FooID`
func legacyFormatName(s string) string {
	if len(s) > 0 {
		s = strings.ToUpper(string(s[0])) + s[1:]
	}
	return lintName(s)
}

// formatArgName formats a GraphQL argument name into a Go parameter name:
// CAMEL with uppercase acronyms, e.g. `filterUri` -> `filterURI`, then
// escaped like formatParamName. Without a formatted name the schema name is
// used as is.
func formatArgName(s string) string {
	if name, ok := engineName(s, camelNames); ok {
		s = name
	}
	return formatParamName(s)
}

// formatModuleName is the Go name of the constructor of a module's main
// object, from the module's name: what formatName gives the constructor's
// schema field.
func formatModuleName(module string) string {
	if name, ok := generator.GetSchema().FormatName(module, pascalNames); ok {
		return name
	}
	return formatName(strcase.ToLowerCamel(module))
}

// enumValueName formats an enum value into the Go suffix of its scoped
// constant name (`<Enum><Value>`): PASCAL with uppercase acronyms, like every
// other Go identifier, so `TCP` becomes `TCP` (strcase gave `Tcp`) and
// `LEAVE_CONFLICT_MARKERS` stays `LeaveConflictMarkers`.
func enumValueName(s string) string {
	if name, ok := engineName(s, pascalNames); ok {
		return name
	}
	return strcase.ToCamel(s)
}
