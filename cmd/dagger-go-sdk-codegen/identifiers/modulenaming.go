package identifiers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ModuleNames are the names the engine gives a module's declarations
// (ModuleSource.naming, dagger/dagger#14996), by the rules of the version it
// names the module by: the engine's identifier naming rules from
// CasingVersion on, strcase before. A lookup returns false only when asking
// the engine failed, and Err returns why: generation fails with it.
type ModuleNames interface {
	// TypeName is the schema name of an object, interface or enum the
	// module declares: namespaced by the module's name (typeNames).
	TypeName(name string) (string, bool)
	// FieldName is the schema name of a function, field or argument
	// (fieldNames).
	FieldName(name string) (string, bool)
	// MainObject is the module's main object among candidates, the objects
	// the module may declare; "" for none (mainObject).
	MainObject(candidates []string) (string, bool)
	// Err is the first failure of a lookup's query.
	Err() error
}

// ModuleNaming is the ModuleNames of a module source. A lookup asks the
// engine directly the first time, and the answer is kept, so only names the
// module's declarations use reach the engine.
type ModuleNaming struct {
	// ctx is the generation's: lookups are made while rendering, which has
	// none of its own.
	ctx    context.Context
	engine Engine
	id     string

	mu sync.Mutex
	// answers holds the names answered, by ModuleNaming field and name.
	answers map[string]map[string]string
	// mains holds mainObject's answers by candidates (see mainKey).
	mains map[string]string
	// err is the first failure of a lookup.
	err error
}

var _ ModuleNames = (*ModuleNaming)(nil)

// ModuleNaming returns the naming of the module source sourceID: the source
// the engine serves the module as, under the name its manifest gives it.
func (c *Connector) ModuleNaming(ctx context.Context, sourceID string) (*ModuleNaming, error) {
	if sourceID == "" {
		return nil, errors.New("naming the module needs its module source ID")
	}
	engine, err := c.engineAtFloor(ctx)
	if err != nil {
		return nil, fmt.Errorf("name the module: %w", err)
	}
	return &ModuleNaming{
		ctx:     ctx,
		engine:  engine,
		id:      sourceID,
		answers: map[string]map[string]string{},
		mains:   map[string]string{},
	}, nil
}

func (m *ModuleNaming) TypeName(name string) (string, bool) { return m.lookup("typeNames", name) }

func (m *ModuleNaming) FieldName(name string) (string, bool) { return m.lookup("fieldNames", name) }

func (m *ModuleNaming) MainObject(candidates []string) (string, bool) {
	if len(candidates) == 0 {
		return "", true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if main, ok := m.mains[mainKey(candidates)]; ok {
		return main, true
	}
	if m.err != nil {
		return "", false
	}
	if err := m.ask(nil, nil, candidates); err != nil {
		m.err = err
		return "", false
	}
	return m.mains[mainKey(candidates)], true
}

func (m *ModuleNaming) lookup(field, name string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if answer, ok := m.answers[field][name]; ok {
		return answer, true
	}
	if m.err != nil {
		return "", false
	}
	lists := map[string][]string{field: {name}}
	if err := m.ask(lists["typeNames"], lists["fieldNames"], nil); err != nil {
		m.err = err
		return "", false
	}
	return m.answers[field][name], true
}

func (m *ModuleNaming) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// mainKey keys mainObject's answers by their candidates.
func mainKey(candidates []string) string {
	return strings.Join(candidates, "\x00")
}

// ask queries the module source's naming in one query, and keeps the
// answers: typeNames and fieldNames when non-empty, and mainObject when
// candidates is non-nil. The caller holds m.mu.
func (m *ModuleNaming) ask(typeNames, fieldNames, candidates []string) error {
	params := []string{"$m0: ID!"}
	vars := map[string]any{"m0": m.id}
	var selection strings.Builder
	for _, list := range []struct {
		field string
		names []string
	}{{"typeNames", typeNames}, {"fieldNames", fieldNames}} {
		if len(list.names) == 0 {
			continue
		}
		v := fmt.Sprintf("l%d", len(vars)-1)
		params = append(params, "$"+v+": [String!]!")
		vars[v] = list.names
		fmt.Fprintf(&selection, " %s(names: $%s)", list.field, v)
	}
	if candidates != nil {
		params = append(params, "$c0: [String!]!")
		vars["c0"] = candidates
		selection.WriteString(" main: mainObject(candidates: $c0)")
	}
	text := fmt.Sprintf("query ModuleNaming(%s) {\n  m0: node(id: $m0) { ... on ModuleSource { naming {%s } } }\n}", strings.Join(params, ", "), selection.String())
	var data struct {
		M0 struct {
			Naming map[string]json.RawMessage `json:"naming"`
		} `json:"m0"`
	}
	if err := m.engine.Query(m.ctx, text, vars, &data); err != nil {
		return fmt.Errorf("name the module: %w", err)
	}
	naming := data.M0.Naming
	if naming == nil {
		return errors.New("name the module: the module source ID names no ModuleSource")
	}
	for field, names := range map[string][]string{"typeNames": typeNames, "fieldNames": fieldNames} {
		if len(names) == 0 {
			continue
		}
		var answers []string
		if err := json.Unmarshal(naming[field], &answers); err != nil {
			return fmt.Errorf("name the module: decode %s: %w", field, err)
		}
		if len(answers) != len(names) {
			return fmt.Errorf("name the module: %s: got %d names for %d", field, len(answers), len(names))
		}
		if m.answers[field] == nil {
			m.answers[field] = map[string]string{}
		}
		for i, name := range names {
			m.answers[field][name] = answers[i]
		}
	}
	if candidates != nil {
		var main *string
		if err := json.Unmarshal(naming["main"], &main); err != nil {
			return fmt.Errorf("name the module: decode mainObject: %w", err)
		}
		m.mains[mainKey(candidates)] = ""
		if main != nil {
			m.mains[mainKey(candidates)] = *main
		}
	}
	return nil
}
