package identifiers

import (
	"context"
	"fmt"
)

// ModuleNames are the schema names the engine gives a module's own
// declarations (Naming.module, dagger/dagger#14996), by the rules of the
// version it serves the module at.
type ModuleNames struct {
	// Types are the schema names of the objects and interfaces the module
	// declares, by Go name: namespaced by the module's name.
	Types map[string]string
	// Fields are the schema names of functions, fields and arguments, by Go
	// name.
	Fields map[string]string
	// Main is the module's main object among the candidates asked about,
	// or "" for none.
	Main string
}

// NameModule asks the engine, in one query, how the rules of version name
// the declarations of the module name, originally originalName (they differ
// for a dependency alias): types, fields, and which of candidates is its
// main object. A name the engine can't parse fails the query.
func NameModule(ctx context.Context, query Query, version, name, originalName string, types, fields, candidates []string) (ModuleNames, error) {
	var data struct {
		Naming struct {
			Module struct {
				TypeNames  []string `json:"typeNames"`
				FieldNames []string `json:"fieldNames"`
				MainObject *string  `json:"mainObject"`
			} `json:"module"`
		} `json:"naming"`
	}
	err := query(ctx, `query ModuleNaming($version: String!, $name: String!, $originalName: String!, $types: [String!]!, $fields: [String!]!, $candidates: [String!]!) {
  naming(version: $version) { module(name: $name, originalName: $originalName) {
    typeNames(names: $types)
    fieldNames(names: $fields)
    mainObject(candidates: $candidates)
  } }
}`, map[string]any{"version": version, "name": name, "originalName": originalName, "types": nonNil(types), "fields": nonNil(fields), "candidates": nonNil(candidates)}, &data)
	if err != nil {
		return ModuleNames{}, fmt.Errorf("name the module: %w", err)
	}
	naming := data.Naming.Module
	names := ModuleNames{Types: map[string]string{}, Fields: map[string]string{}}
	for _, list := range []struct {
		asked, answered []string
		into            map[string]string
	}{{types, naming.TypeNames, names.Types}, {fields, naming.FieldNames, names.Fields}} {
		if len(list.answered) != len(list.asked) {
			return ModuleNames{}, fmt.Errorf("name the module: got %d names for %d", len(list.answered), len(list.asked))
		}
		for i, name := range list.asked {
			list.into[name] = list.answered[i]
		}
	}
	if naming.MainObject != nil {
		names.Main = *naming.MainObject
	}
	return names, nil
}

// nonNil keeps an empty list a list in the query's variables: null isn't a
// [String!]!.
func nonNil(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}
