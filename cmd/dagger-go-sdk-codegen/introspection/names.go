package introspection

import (
	"sort"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/internal/naming"
)

// Codegen names SDK identifiers like the engine's Query.formatIdentifiers
// does, with the naming rules of the schema's engine version.
//
// The gate is the schema being generated and the engine that served it (see
// NamesIdentifiers): engines name identifiers with the new rules from
// v1.0.0-beta.21, and serve Query.formatIdentifiers exactly at those views.
// Otherwise codegen keeps its legacy converters, so older schemas generate
// byte-identical code.
//
// go-sdk's generators run without an engine connection, so the names are
// formatted with internal/naming, a copy of the engine's rules, rather than
// by asking the engine.

// NameFormat is a casing with an acronym style, like the casing and acronyms
// arguments of Query.formatIdentifiers.
type NameFormat struct {
	Casing   naming.Casing
	Acronyms naming.AcronymStyle
}

func (f NameFormat) String() string {
	return f.Casing.String() + ":" + f.Acronyms.String()
}

// HasFormatIdentifiers reports whether the schema has Query.formatIdentifiers.
func (s *Schema) HasFormatIdentifiers() bool {
	if s == nil {
		return false
	}
	query := s.Query()
	if query == nil {
		return false
	}
	for _, f := range query.Fields {
		if f.Name == "formatIdentifiers" {
			return true
		}
	}
	return false
}

// IdentifierNamingVersion is the first engine version that names identifiers
// by engine/naming, at the views from that version on
// (engine.IdentifierCasingVersion in dagger/dagger).
const IdentifierNamingVersion = "v1.0.0-beta.21"

// NamesIdentifiers reports whether the schema's identifiers follow the
// engine's naming rules, served by an engine of engineVersion (Query.version):
// the gate for codegen to apply them. Engines from IdentifierNamingVersion on
// serve Query.formatIdentifiers exactly at those views. Released engines
// before it (v1.0.0-beta.19 and v1.0.0-beta.20) serve the field too, at every
// v1.0.0 view, while still naming by strcase, and report each of those views
// as v1.0.0, so the schema can't tell: the engine's version must be at least
// IdentifierNamingVersion too. An empty or unparseable engine version is
// unknown, and keeps the legacy names.
func (s *Schema) NamesIdentifiers(engineVersion string) bool {
	if !s.HasFormatIdentifiers() {
		return false
	}
	v, ok := engineReleaseVersion(engineVersion)
	if !ok {
		return false
	}
	return semver.Compare(v, IdentifierNamingVersion) >= 0
}

// engineReleaseVersion normalizes an engine's version for comparison: build
// metadata dropped, and a development build (a prerelease mentioning "dev",
// such as v1.0.0-dev-123) as its release, like the engine's views treat it.
func engineReleaseVersion(version string) (string, bool) {
	v := version
	if v == "" {
		return "", false
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	v = strings.TrimSuffix(v, semver.Build(v))
	if pre := semver.Prerelease(v); strings.Contains(pre, "dev") {
		v = strings.TrimSuffix(v, pre)
	}
	return v, true
}

// Names returns the distinct type, field, argument, input field and enum
// value names in the schema, sorted. It leaves out introspection names ("__"
// prefix, including every name inside an introspection type).
func (s *Schema) Names() []string {
	seen := map[string]bool{}
	add := func(name string) {
		if !strings.HasPrefix(name, "__") {
			seen[name] = true
		}
	}
	for _, t := range s.Types {
		if strings.HasPrefix(t.Name, "__") {
			continue
		}
		add(t.Name)
		for _, f := range t.Fields {
			add(f.Name)
			for _, arg := range f.Args {
				add(arg.Name)
			}
		}
		for _, f := range t.InputFields {
			add(f.Name)
		}
		for _, v := range t.EnumValues {
			add(v.Name)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LoadFormattedNames formats every name of the schema (see Names) in each of
// formats, with the naming rules of schemaVersion, the version of the schema
// being generated, and keeps the results on the schema for FormattedName. A
// schema that doesn't name identifiers by the engine's rules, as served by
// an engine of engineVersion (see NamesIdentifiers), is left as is, so
// FormattedName reports false and codegen falls back to its legacy
// converters. It fails for a schema version newer than the rules codegen
// knows (see naming.CheckKnownVersion).
func (s *Schema) LoadFormattedNames(schemaVersion, engineVersion string, formats ...NameFormat) error {
	if !s.NamesIdentifiers(engineVersion) {
		return nil
	}
	if err := naming.CheckKnownVersion(schemaVersion); err != nil {
		return err
	}
	dict := naming.DictionaryFor(schemaVersion)
	ids := map[string]naming.Identifier{}
	for _, name := range s.Names() {
		// The engine can't format a name without letters or digits.
		if id, err := dict.Parse(name); err == nil {
			ids[name] = id
		}
	}
	if s.FormattedNames == nil {
		s.FormattedNames = map[NameFormat]map[string]string{}
	}
	s.namingDict = dict
	for _, f := range formats {
		formatted := make(map[string]string, len(ids))
		for name, id := range ids {
			formatted[name] = id.Format(f.Casing, f.Acronyms)
		}
		s.FormattedNames[f] = formatted
	}
	return nil
}

// FormattedName returns a schema name formatted in f. It reports false when
// the name wasn't formatted in f: the schema predates Query.formatIdentifiers,
// f wasn't loaded, or the name isn't one of the schema's names (see Names).
// Codegen then uses its legacy converter.
func (s *Schema) FormattedName(name string, f NameFormat) (string, bool) {
	if s == nil {
		return "", false
	}
	formatted, ok := s.FormattedNames[f][name]
	return formatted, ok
}

// FormatName formats any name in f with the rules FormattedNames were
// formatted with, for names that aren't the schema's own, such as a module's
// name. It reports false when the schema has no formatted names (see
// LoadFormattedNames), or the name can't be formatted.
func (s *Schema) FormatName(name string, f NameFormat) (string, bool) {
	if s == nil || s.namingDict == nil {
		return "", false
	}
	id, err := s.namingDict.Parse(name)
	if err != nil {
		return "", false
	}
	return id.Format(f.Casing, f.Acronyms), true
}
