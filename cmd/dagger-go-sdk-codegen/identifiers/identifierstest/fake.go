// Package identifierstest fakes the engine's naming for generator tests.
package identifierstest

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
)

// Names are Go identifiers formatted by Query.
func Names() *identifiers.Names {
	return identifiers.NewNames(context.Background(), Query, "v1.0.0-beta.22")
}

// Query is a fake engine that formats Go identifiers by Format (see
// Formatting).
var Query = Formatting(Format)

// Formatting returns a fake engine that answers the queries of package
// identifiers: formatIdentifiers by format, and ModuleSource.naming as the
// engine names, by strcase, a module named by its source ID.
func Formatting(format func(string) string) identifiers.Query {
	return func(_ context.Context, query string, variables map[string]any, data any) error {
		var result any
		switch {
		case strings.Contains(query, "formatIdentifiers("):
			var formatted []string
			for _, name := range variables["names"].([]string) {
				formatted = append(formatted, format(name))
			}
			result = map[string]any{"formatIdentifiers": formatted}
		case strings.Contains(query, "naming {"):
			result = map[string]any{"node": map[string]any{"naming": naming(variables)}}
		default:
			return fmt.Errorf("fake engine: unexpected query %q", query)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, data)
	}
}

// acronyms are the words Format spells in uppercase.
var acronyms = []string{"API", "GPU", "HTML", "HTTP", "ID", "JSON", "LLM", "SDK", "SHA", "SSH", "SSHFS", "URL"}

// Format is a stand-in for the engine's rules, enough for the names tests
// spell: PascalCase, with acronyms (and their plurals) in uppercase.
func Format(name string) string {
	var b strings.Builder
	for _, word := range strings.Split(strcase.ToSnake(name), "_") {
		upper := strings.ToUpper(word)
		switch {
		case slices.Contains(acronyms, upper):
			b.WriteString(upper)
		case strings.HasSuffix(word, "s") && slices.Contains(acronyms, upper[:len(upper)-1]):
			b.WriteString(upper[:len(upper)-1] + "s")
		default:
			b.WriteString(strcase.ToCamel(word))
		}
	}
	return b.String()
}

// naming answers ModuleSource.naming for the module named by its source ID.
func naming(variables map[string]any) map[string]any {
	module := strcase.ToCamel(variables["id"].(string))
	var types, fields []string
	for _, name := range variables["types"].([]string) {
		if rest, ok := strings.CutPrefix(strcase.ToCamel(name), module); ok && (rest == "" || 'A' <= rest[0] && rest[0] <= 'Z') {
			types = append(types, strcase.ToCamel(name))
		} else {
			types = append(types, module+strcase.ToCamel(name))
		}
	}
	for _, name := range variables["fields"].([]string) {
		fields = append(fields, strcase.ToLowerCamel(name))
	}
	var main any
	for _, name := range variables["candidates"].([]string) {
		if strcase.ToCamel(name) == module {
			main = name
		}
	}
	return map[string]any{"typeNames": types, "fieldNames": fields, "mainObject": main}
}
