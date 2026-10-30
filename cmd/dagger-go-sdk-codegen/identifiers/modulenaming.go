package identifiers

import (
	"context"
	"errors"
	"fmt"
)

// ModuleNames are the schema names the engine gives a module's own
// declarations (ModuleSource.naming, dagger/dagger#14996), by the rules of
// the version it serves the module at.
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

// NameModule asks the engine, in one query, how it names the module source
// sourceID's declarations: types, fields, and which of candidates is its
// main object. A name the engine can't parse fails the query.
func NameModule(ctx context.Context, query Query, sourceID string, types, fields, candidates []string) (ModuleNames, error) {
	if sourceID == "" {
		return ModuleNames{}, errors.New("name the module: no module source ID")
	}
	var data struct {
		Node *struct {
			Naming struct {
				TypeNames  []string `json:"typeNames"`
				FieldNames []string `json:"fieldNames"`
				MainObject *string  `json:"mainObject"`
			} `json:"naming"`
		} `json:"node"`
	}
	err := query(ctx, `query ModuleNaming($id: ID!, $types: [String!]!, $fields: [String!]!, $candidates: [String!]!) {
  node(id: $id) { ... on ModuleSource { naming {
    typeNames(names: $types)
    fieldNames(names: $fields)
    mainObject(candidates: $candidates)
  } } }
}`, map[string]any{"id": sourceID, "types": nonNil(types), "fields": nonNil(fields), "candidates": nonNil(candidates)}, &data)
	if err != nil {
		return ModuleNames{}, fmt.Errorf("name the module: %w", err)
	}
	if data.Node == nil {
		return ModuleNames{}, errors.New("name the module: the module source ID names no ModuleSource")
	}
	naming := data.Node.Naming
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
