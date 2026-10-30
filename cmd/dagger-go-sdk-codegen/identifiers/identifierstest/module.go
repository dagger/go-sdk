package identifierstest

import (
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
)

// Module is a module's naming, as a fake ModuleSource.naming answers it. It
// is an identifiers.ModuleNames too, answering at once, for tests of
// generators without the batches.
type Module struct {
	// Names holds the answers of typeNames and fieldNames, by field and name.
	Names map[string]map[string]string
	// Fallback answers a name Names lacks; without it, such a name has no
	// answer, which fails the query.
	Fallback func(field, name string) string
	// Main answers mainObject; "" (or a nil Main) is null.
	Main func(candidates []string) string
}

var _ identifiers.ModuleNames = (*Module)(nil)

func (m *Module) answer(field, name string) (string, bool) {
	if answer, ok := m.Names[field][name]; ok {
		return answer, true
	}
	if m.Fallback != nil {
		return m.Fallback(field, name), true
	}
	return "", false
}

func (m *Module) TypeName(name string) (string, bool)  { return m.answer("typeNames", name) }
func (m *Module) FieldName(name string) (string, bool) { return m.answer("fieldNames", name) }
func (m *Module) MainObject(candidates []string) (string, bool) {
	if m.Main == nil {
		return "", true
	}
	return m.Main(candidates), true
}

// Strcase is the naming the engine gives a module named moduleName by the
// legacy strcase rules (LegacyNamer in dagger/dagger core/gqlformat.go).
func Strcase(moduleName string) *Module {
	main := strcase.ToCamel(moduleName)
	return &Module{
		Fallback: func(field, name string) string {
			if field == "fieldNames" {
				return strcase.ToLowerCamel(name)
			}
			return legacyNamespaceObject(name, moduleName)
		},
		Main: func(candidates []string) string {
			for _, name := range candidates {
				if strcase.ToCamel(name) == main {
					return name
				}
			}
			return ""
		},
	}
}

// legacyNamespaceObject is the engine's legacyNamespaceObject, for a module
// whose final and original names are the same.
func legacyNamespaceObject(typeName, moduleName string) string {
	typeName = strcase.ToCamel(typeName)
	modName := strcase.ToCamel(moduleName)
	if rest := strings.TrimPrefix(typeName, modName); rest != typeName {
		if len(rest) == 0 {
			return modName
		}
		if 'A' <= rest[0] && rest[0] <= 'Z' {
			return typeName
		}
	}
	return strcase.ToCamel(modName + "_" + typeName)
}
