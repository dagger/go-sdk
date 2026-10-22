package templates

import (
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/internal/naming"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// This file names the module's own types, fields and arguments as the engine
// does, for the strings the module side sends over the wire or merges into
// the schema. naming.go formats Go identifiers from schema names; this is the
// other direction, from Go source names to schema names. Ported from
// dagger/dagger's cmd/codegen (module_naming.go, dagger/dagger#14669).

// moduleNamer names the module's own types, fields, arguments and enum
// members the way the engine does for the module (see core.Namer in
// dagger/dagger core/gqlformat.go): engine/naming with the dictionary of the
// module's schema version when the engine names the module with it, strcase
// otherwise. The bindings the module calls itself through, and the schema it
// merges its types into, must use the engine's names or the references
// break: a module named by the new rules that references JSONValue or
// LLMMessage doesn't build with strcase's JsonValue and LlmMessage.
//
// The gate is the one the Go identifiers use (see naming.go and
// introspection.Schema.NamesIdentifiers): the schema the module is generated
// from has Query.formatIdentifiers, which the engine hides from the views of
// modules it names by the legacy rules, and the engine is v1.0.0-beta.21 or
// later, since older engines serve the field while still naming by strcase.
//
// The zero moduleNamer is the legacy strcase one.
type moduleNamer struct {
	dict *naming.Dictionary
	// schema is the schema of the module's dependencies (core included),
	// for resolving references to their types like the engine does.
	schema *introspection.Schema
}

func (funcs goTemplateFuncs) moduleNamer() moduleNamer {
	return newModuleNamer(funcs.fullSchema, funcs.schema, funcs.schemaVersion, funcs.cfg.EngineVersion)
}

func newModuleNamer(fullSchema, schema *introspection.Schema, schemaVersion, engineVersion string) moduleNamer {
	if fullSchema != nil {
		schema = fullSchema
	}
	if !schema.NamesIdentifiers(engineVersion) {
		return moduleNamer{schema: schema}
	}
	// No version is the latest, as for the engine's views.
	return moduleNamer{dict: naming.DictionaryFor(schemaVersion), schema: schema}
}

// ModuleConstructorNames returns the schema names the engine gives the
// constructor field of a module and its main object, for a module generated
// from schema at schemaVersion, served by an engine of engineVersion.
func ModuleConstructorNames(schema *introspection.Schema, schemaVersion, engineVersion, moduleName string) (field, object string) {
	n := newModuleNamer(schema, nil, schemaVersion, engineVersion)
	return n.fieldName(moduleName), n.objectName(moduleName)
}

// typeName maps a parsed type name to the name the engine installs, or
// resolves, it under. Module-local types (moduleName != "") are namespaced
// with the module name, as the engine does when installing the module's
// typedefs — without this, a module type shadowing a core or dependency type
// name would collide at merge time instead of being namespaced.
//
// A core or dependency type is referred to by its Go binding's name. Legacy
// modules use strcase's spelling of it. With the new rules the engine
// normalizes the name, and if that misses the schema, falls back to the type
// with the same normalized name (see Module.depTypeReference in dagger/dagger).
func (n moduleNamer) typeName(name, moduleName string) string {
	if moduleName != "" {
		return n.namespaceTypeName(name, moduleName)
	}
	if n.dict == nil {
		return strcase.ToCamel(name)
	}
	normalized := n.objectName(name)
	if n.schema == nil || n.schema.Types.Get(normalized) != nil {
		return normalized
	}
	for _, t := range n.schema.Types {
		if t.Name == name || n.objectName(t.Name) == normalized {
			return t.Name
		}
	}
	return normalized
}

func (n moduleNamer) format(name string, casing naming.Casing) string {
	id, err := n.dict.Parse(name)
	if err != nil {
		return name
	}
	return id.Format(casing, naming.Uppercase)
}

// objectName is the schema name of an object, interface or enum.
func (n moduleNamer) objectName(name string) string {
	if n.dict == nil {
		return strcase.ToCamel(name)
	}
	return n.format(name, naming.Pascal)
}

// fieldName is the schema name of a field, function or argument.
func (n moduleNamer) fieldName(name string) string {
	if n.dict == nil {
		return strcase.ToLowerCamel(name)
	}
	return n.format(name, naming.Camel)
}

// enumMemberName mirrors the engine's enum member naming (gqlEnumMemberName
// in dagger/dagger core/gqlformat.go): names already in GraphQL's member
// convention (HTTP2, P_256) are kept, everything else becomes SCREAMING_SNAKE.
// Emitting a different name than the engine registers would make self-call
// bindings send unknown enum values.
func (n moduleNamer) enumMemberName(name string) string {
	if isConventionalGraphQLEnumMemberName(name) {
		return name
	}
	if n.dict == nil {
		return strcase.ToScreamingSnake(name)
	}
	return n.format(name, naming.ScreamingSnake)
}

// namespaceTypeName mirrors the engine's NamespaceObject (core/gqlformat.go),
// which the engine applies to module objects, interfaces and enums alike, for
// the case where the module's final and original names are equal — always
// true for the module's own view of itself. Keep in sync.
func (n moduleNamer) namespaceTypeName(typeName, moduleName string) string {
	if n.dict == nil {
		typeName = strcase.ToCamel(typeName)
		modName := strcase.ToCamel(moduleName)
		if rest := strings.TrimPrefix(typeName, modName); rest != typeName {
			if len(rest) == 0 {
				// The main module object keeps the module's name.
				return modName
			}
			// Only treat the prefix as a namespace on a word boundary: type
			// "Postman" in module "post" must become "PostPostman", while
			// "PostMan" is already namespaced.
			if 'A' <= rest[0] && rest[0] <= 'Z' {
				return typeName
			}
		}
		return strcase.ToCamel(modName + "_" + typeName)
	}
	typ, err := n.dict.Parse(typeName)
	if err != nil {
		return n.objectName(moduleName + "_" + typeName)
	}
	mod, err := n.dict.Parse(moduleName)
	if err != nil {
		return n.objectName(moduleName + "_" + typeName)
	}
	// The prefix check compares words, so it only matches on a word
	// boundary. The words may be cut differently in the type's and the
	// module's spelling around digits and acronyms (K8STools in module
	// k8s-tools, My2Go in module my2go), so failing that, words that spell
	// the module's name once flattened count as the prefix too.
	words := typ.Words
	if hasWordPrefix(typ.Words, mod.Words) {
		words = typ.Words[len(mod.Words):]
	} else if k := flatWordPrefix(typ.Words, flatName(moduleName)); k >= 0 {
		words = typ.Words[k:]
	}
	words = append(append([]naming.Word(nil), mod.Words...), words...)
	return naming.Identifier{Words: words}.Format(naming.Pascal, naming.Uppercase)
}

// sameDeclaredName mirrors the engine's Namer.SameDeclaredName: whether a and
// b, two spellings of a declared name, name the same thing, such as a module's
// name and its main type. They normalize to the same schema name, or, with
// the engine/naming rules, they're the same once flattened: a template may
// spell the main type differently around digits and acronyms than the
// module's name (K8STools for k8s-tools, My2Go for my2go), which strcase
// happened to normalize alike but engine/naming doesn't.
func (n moduleNamer) sameDeclaredName(a, b string) bool {
	if n.objectName(a) == n.objectName(b) {
		return true
	}
	return n.dict != nil && flatName(a) != "" && flatName(a) == flatName(b)
}

// flatName is name lowercased, with everything but ASCII letters and digits
// dropped: the spelling every casing of a name shares.
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

// flatWordPrefix returns how many of words, flattened and joined, spell
// prefix exactly, ending on a word boundary; or -1.
func flatWordPrefix(words []naming.Word, prefix string) int {
	if prefix == "" {
		return -1
	}
	var spelled string
	for i, w := range words {
		spelled += flatName(w.Text + w.Suffix)
		switch {
		case spelled == prefix:
			return i + 1
		case len(spelled) >= len(prefix) || !strings.HasPrefix(prefix, spelled):
			return -1
		}
	}
	return -1
}

func hasWordPrefix(words, prefix []naming.Word) bool {
	if len(prefix) > len(words) {
		return false
	}
	for i, w := range prefix {
		if !strings.EqualFold(words[i].Text, w.Text) || !strings.EqualFold(words[i].Suffix, w.Suffix) {
			return false
		}
	}
	return true
}
