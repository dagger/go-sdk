package templates

import (
	"go/types"
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
)

// moduleNamer predicts the schema names the engine gives the module's own
// declarations: the names its interface bindings select and its entrypoint's
// main object. It mirrors core.Namer in dagger/dagger (core/gqlformat.go),
// which names a module by the rules of the engine version it's served at
// (ModuleSource.NamingView): engine/naming from identifiers.CasingVersion on,
// strcase before it. A name the bindings predict differently than the engine
// names it breaks the module: an interface binding selects a field or
// fragment the schema lacks.
//
// This is the opposite direction of goNamer: goNamer styles schema names as
// Go identifiers, which is the SDK's choice; moduleNamer predicts schema
// names from Go source names, which is the engine's. So their rules come
// from different versions: goNamer follows the generated schema's version,
// moduleNamer the version the engine names the module's declarations by.
//
// The zero moduleNamer is the legacy strcase one.
type moduleNamer struct {
	// names are formatted by the rules the engine names the module's
	// declarations by, or nil for strcase.
	names *identifiers.Names
}

// moduleNamer returns the namer of the module being generated.
func (funcs goTemplateFuncs) moduleNamer() moduleNamer {
	var n moduleNamer
	if funcs.cfg.ModuleConfig != nil {
		n.names = funcs.cfg.ModuleConfig.Names
	}
	return n
}

func (n moduleNamer) legacy() bool {
	return n.names == nil
}

// format returns name formatted in f. A name the engine can't parse stays as
// it is, as the engine leaves it (schema validation reports it). A name that
// isn't formatted yet returns false: it's formatted before the next render
// (identifiers.Names.Run), and the caller stands in its legacy name.
func (n moduleNamer) format(f identifiers.Format, name string) (string, bool) {
	if !identifiers.Valid(name) {
		return name, true
	}
	return n.names.Lookup(f, name)
}

// ObjectName is the schema name of an object, interface, enum or scalar:
// PascalCase, with acronyms in capitals (HTTPClient).
func (n moduleNamer) ObjectName(name string) string {
	if !n.legacy() {
		if formatted, ok := n.format(identifiers.TypeName, name); ok {
			return formatted
		}
	}
	return strcase.ToCamel(name)
}

// FieldName is the schema name of a field, function or argument: camelCase,
// with acronyms written like words (baseUrl, httpClient).
func (n moduleNamer) FieldName(name string) string {
	if !n.legacy() {
		if formatted, ok := n.format(identifiers.FieldName, name); ok {
			return formatted
		}
	}
	return strcase.ToLowerCamel(name)
}

// namespaceObject maps the name of a type the module declares to its schema
// name: the module's name, followed by the type's name unless that already
// starts with the module's name. It mirrors Namer.NamespaceObject, for the
// module's view of itself, whose final and original names are the same.
func (n moduleNamer) namespaceObject(typeName, moduleName string) string {
	if n.legacy() {
		return legacyNamespaceObject(typeName, moduleName)
	}
	if !identifiers.Valid(typeName) || !identifiers.Valid(moduleName) {
		return n.ObjectName(moduleName + "_" + typeName)
	}
	typePascal, ok1 := n.names.Lookup(identifiers.TypeName, typeName)
	typeWords, ok2 := n.names.Lookup(identifiers.Words, typeName)
	modPascal, ok3 := n.names.Lookup(identifiers.TypeName, moduleName)
	modWords, ok4 := n.names.Lookup(identifiers.Words, moduleName)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return legacyNamespaceObject(typeName, moduleName)
	}
	// The engine compares words, not characters, so the module's name is
	// only a prefix on a word boundary: "Postman" in module "post" becomes
	// "PostPostman", while "PostMan" is already namespaced. Failing that,
	// words that spell the module's name once flattened count too, since
	// the type and the module may cut words differently around digits and
	// acronyms (K8STools in module k8s-tools, My2Go in module my2go).
	//
	// The engine works on parsed words; formatIdentifiers returns strings.
	// The KEBAB form gives the words (lowercase, as the engine compares them
	// case-insensitively), and the PASCAL form is the concatenation of each
	// word's capitalized form, which has the same length as the word, so it
	// splits along the same boundaries.
	words := strings.Split(typeWords, "-")
	rest := 0
	if mod := strings.Split(modWords, "-"); hasWordPrefix(words, mod) {
		rest = len(mod)
	} else if k := flatWordPrefix(words, flatName(moduleName)); k >= 0 {
		rest = k
	}
	offset := 0
	for _, w := range words[:rest] {
		offset += len(w)
	}
	if offset > len(typePascal) {
		// Unreachable unless formatIdentifiers changes its output.
		return modPascal + typePascal
	}
	return modPascal + typePascal[offset:]
}

// legacyNamespaceObject is namespaceObject by the strcase rules (the
// engine's legacyNamespaceObject), unchanged from before engine/naming.
func legacyNamespaceObject(typeName, moduleName string) string {
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

// hasWordPrefix reports whether words starts with prefix.
func hasWordPrefix(words, prefix []string) bool {
	if len(prefix) > len(words) {
		return false
	}
	for i, w := range prefix {
		if words[i] != w {
			return false
		}
	}
	return true
}

// flatWordPrefix returns how many of words, flattened and joined, spell
// prefix exactly, ending on a word boundary; or -1.
func flatWordPrefix(words []string, prefix string) int {
	if prefix == "" {
		return -1
	}
	var spelled string
	for i, w := range words {
		spelled += flatName(w)
		switch {
		case spelled == prefix:
			return i + 1
		case len(spelled) >= len(prefix) || !strings.HasPrefix(prefix, spelled):
			return -1
		}
	}
	return -1
}

// sameDeclaredName mirrors Namer.SameDeclaredName: whether a and b, two
// spellings of a declared name, name the same thing, such as a module's name
// and its main object. They normalize to the same schema name, or, with the
// engine/naming rules, they're the same once flattened: a template may spell
// the main object differently around digits and acronyms than the module's
// name (K8STools for k8s-tools, My2Go for my2go).
func (n moduleNamer) sameDeclaredName(a, b string) bool {
	if n.ObjectName(a) == n.ObjectName(b) {
		return true
	}
	return !n.legacy() && flatName(a) != "" && flatName(a) == flatName(b)
}

// isMainObject reports whether the object named name is the module's main
// object, as the engine finds it (Module.objectTypeDefResultByOriginalName):
// the object whose name normalizes like the module's, or else the only one
// that's the same once flattened. objects are the names of every object the
// module may declare.
func (n moduleNamer) isMainObject(name, moduleName string, objects []string) bool {
	if n.ObjectName(name) == n.ObjectName(moduleName) {
		return true
	}
	if !n.sameDeclaredName(name, moduleName) {
		return false
	}
	for _, other := range objects {
		if other != name && n.sameDeclaredName(other, moduleName) {
			// Another exact match wins, and several flat ones match nothing.
			return false
		}
	}
	return true
}

// packageObjectNames lists the exported named struct types of a package: the
// objects a module may declare.
func packageObjectNames(pkg *types.Package) []string {
	if pkg == nil {
		return nil
	}
	var names []string
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() {
			continue
		}
		if _, ok := obj.Type().Underlying().(*types.Struct); ok {
			names = append(names, name)
		}
	}
	return names
}
