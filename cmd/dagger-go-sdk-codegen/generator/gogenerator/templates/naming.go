package templates

import (
	"slices"
	"strings"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// goNamer formats GraphQL names (types, fields, arguments) as exported Go
// identifiers, like dagger/dagger's in-repo Go generator (goNamer in
// cmd/codegen/generator/go/templates/naming.go). Wire names are never
// formatted: selections, argument keys, input fields and enum values keep the
// schema's spelling.
type goNamer struct {
	// names are the names formatted by the engine's naming rules of the
	// generated schema's version (see identifiers.Connector): PascalCase with
	// uppercase acronyms, so commitSha and commitSHA are both CommitSHA, and
	// withGpu is WithGPU. Nil for the legacy rules, whose identifiers keep
	// the golint-style initialisms (lintName) byte for byte.
	names *identifiers.Names
}

// formatName formats a GraphQL name (e.g. object, field, arg) into a Go
// identifier. Example: `fooId` -> `FooID`
//
// With the engine's rules, a name the schema doesn't declare (FooID) is
// formatted with a direct call when first looked up; if that fails, the
// legacy spelling stands in and the generator returns the error
// (identifiers.Names.Err). Internal names (__typename) keep the legacy
// spelling, as in dagger/dagger.
func (n goNamer) formatName(s string) string {
	if n.names != nil && s != "" && !strings.HasPrefix(s, "_") {
		if name, ok := n.names.Lookup(identifiers.GoName, s); ok {
			return name
		}
	}
	return legacyFormatName(s)
}

// DeclaredNames lists the names schema declares that bindings format as Go
// identifiers: its types, fields, arguments and input fields, less internal
// names (__typename), which keep their legacy spelling. Generators format
// them up front, in one query (identifiers.Names.Prefetch).
func DeclaredNames(schema *introspection.Schema) map[identifiers.Format][]string {
	var declared []string
	for _, typ := range schema.Types {
		declared = append(declared, typ.Name)
		for _, field := range typ.Fields {
			declared = append(declared, field.Name)
			for _, arg := range field.Args {
				declared = append(declared, arg.Name)
			}
		}
		for _, field := range typ.InputFields {
			declared = append(declared, field.Name)
		}
	}
	declared = slices.DeleteFunc(declared, func(name string) bool { return name == "" || strings.HasPrefix(name, "_") })
	return map[identifiers.Format][]string{identifiers.GoName: declared}
}

// legacyFormatName formats a GraphQL name into a Go identifier with
// golint-style initialisms, as for schemas before identifiers.CasingVersion.
// Example: `fooId` -> `FooID`
func legacyFormatName(s string) string {
	if len(s) > 0 {
		s = strings.ToUpper(string(s[0])) + s[1:]
	}
	return lintName(s)
}

// flatName is name lowercased, with everything but ASCII letters and digits
// dropped: the spelling every casing of a name shares, however a naming
// rule cuts it into words (K8STools, K8sTools and k8s-tools are k8stools).
func flatName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case 'a' <= c && c <= 'z', '0' <= c && c <= '9':
			b.WriteByte(c)
		case 'A' <= c && c <= 'Z':
			b.WriteByte(c + 'a' - 'A')
		}
	}
	return b.String()
}
