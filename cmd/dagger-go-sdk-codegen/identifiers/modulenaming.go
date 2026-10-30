package identifiers

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// ModuleNames are the names the engine gives a module's declarations
// (ModuleSource.naming), by the rules of the version it serves the module at,
// on either side of the cut. A lookup the engine hasn't answered yet returns
// false.
type ModuleNames interface {
	// TypeName is the schema name of an object, interface or enum the
	// module declares: namespaced by the module's name.
	TypeName(name string) (string, bool)
	// FieldName is the schema name of a field, function or argument.
	FieldName(name string) (string, bool)
	// EnumMemberName is the schema name of an enum member.
	EnumMemberName(name string) (string, bool)
	// TypeReference is the schema name a reference to a core or dependency
	// type resolves to.
	TypeReference(name string) (string, bool)
	// MainObject is the module's main object among candidates, the objects
	// the module may declare; "" for none.
	MainObject(candidates []string) (string, bool)
	// MainObjectName is the schema name of the module's main object.
	MainObjectName() (string, bool)
}

// ModuleNaming is the ModuleNames of a module source, looked up while a
// generator renders and answered in batches by Run, like Names.
type ModuleNaming struct {
	engine Engine
	id     string

	mu sync.Mutex
	// answers holds the names answered, by ModuleNaming field and name.
	answers map[string]map[string]string
	// pending holds the names looked up but not answered yet.
	pending map[string]map[string]bool
	// mains holds mainObject's answers by candidates (see candidatesKey);
	// pendingMains the candidates not answered yet.
	mains        map[string]string
	pendingMains map[string][]string
	// mainName is mainObjectName, once answered.
	mainName *string
}

// The ModuleNaming fields that map a list of names.
var namingFields = []string{"typeNames", "fieldNames", "enumMemberNames", "typeReferences"}

// ModuleNaming returns the naming of the module source sourceID.
func (c *Connector) ModuleNaming(sourceID string) (*ModuleNaming, error) {
	if sourceID == "" {
		return nil, errors.New("naming the module needs its module source ID")
	}
	engine, err := c.connect()
	if err != nil {
		return nil, err
	}
	return &ModuleNaming{
		engine:       engine,
		id:           sourceID,
		answers:      map[string]map[string]string{},
		pending:      map[string]map[string]bool{},
		mains:        map[string]string{},
		pendingMains: map[string][]string{},
	}, nil
}

func (m *ModuleNaming) TypeName(name string) (string, bool) { return m.lookup("typeNames", name) }

func (m *ModuleNaming) FieldName(name string) (string, bool) { return m.lookup("fieldNames", name) }

func (m *ModuleNaming) EnumMemberName(name string) (string, bool) {
	return m.lookup("enumMemberNames", name)
}

func (m *ModuleNaming) TypeReference(name string) (string, bool) {
	return m.lookup("typeReferences", name)
}

func (m *ModuleNaming) MainObject(candidates []string) (string, bool) {
	key := strings.Join(candidates, "\x00")
	m.mu.Lock()
	defer m.mu.Unlock()
	if main, ok := m.mains[key]; ok {
		return main, true
	}
	m.pendingMains[key] = slices.Clone(candidates)
	return "", false
}

func (m *ModuleNaming) MainObjectName() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mainName == nil {
		m.pending["mainObjectName"] = map[string]bool{"": true}
		return "", false
	}
	return *m.mainName, true
}

func (m *ModuleNaming) lookup(field, name string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if answer, ok := m.answers[field][name]; ok {
		return answer, true
	}
	if m.pending[field] == nil {
		m.pending[field] = map[string]bool{}
	}
	m.pending[field][name] = true
	return "", false
}

func (m *ModuleNaming) session() Engine {
	if m == nil {
		return nil
	}
	return m.engine
}

// add adds a node field selecting the module source's naming: a field per
// list of pending names, an aliased mainObject per set of candidates, and
// mainObjectName.
func (m *ModuleNaming) add(q *query) func(map[string]json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	lists := map[string][]string{}
	for _, field := range namingFields {
		if len(m.pending[field]) > 0 {
			lists[field] = slices.Sorted(maps.Keys(m.pending[field]))
		}
	}
	mainName := len(m.pending["mainObjectName"]) > 0
	mains := slices.Sorted(maps.Keys(m.pendingMains))
	if len(lists) == 0 && len(mains) == 0 && !mainName {
		return nil
	}
	alias := q.param("m", "ID!", m.id)
	var selection strings.Builder
	for _, field := range namingFields {
		if names, ok := lists[field]; ok {
			fmt.Fprintf(&selection, " %s(names: $%s)", field, q.param("l", "[String!]!", names))
		}
	}
	for i, key := range mains {
		fmt.Fprintf(&selection, " main%d: mainObject(candidates: $%s)", i, q.param("c", "[String!]!", m.pendingMains[key]))
	}
	if mainName {
		selection.WriteString(" mainObjectName")
	}
	fmt.Fprintf(&q.fields, "  %s: node(id: $%s) { ... on ModuleSource { naming {%s } } }\n", alias, alias, selection.String())
	return func(data map[string]json.RawMessage) error {
		var node struct {
			Naming map[string]json.RawMessage `json:"naming"`
		}
		if err := json.Unmarshal(data[alias], &node); err != nil {
			return fmt.Errorf("decode %s: %w", alias, err)
		}
		if node.Naming == nil {
			return errors.New("the module source ID names no ModuleSource")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		for field, names := range lists {
			var answers []string
			if err := json.Unmarshal(node.Naming[field], &answers); err != nil {
				return fmt.Errorf("decode %s: %w", field, err)
			}
			if len(answers) != len(names) {
				return fmt.Errorf("%s: got %d names for %d", field, len(answers), len(names))
			}
			if m.answers[field] == nil {
				m.answers[field] = map[string]string{}
			}
			for i, name := range names {
				m.answers[field][name] = answers[i]
				delete(m.pending[field], name)
			}
		}
		for i, key := range mains {
			var main *string
			if err := json.Unmarshal(node.Naming[fmt.Sprintf("main%d", i)], &main); err != nil {
				return fmt.Errorf("decode mainObject: %w", err)
			}
			m.mains[key] = ""
			if main != nil {
				m.mains[key] = *main
			}
			delete(m.pendingMains, key)
		}
		if mainName {
			if err := json.Unmarshal(node.Naming["mainObjectName"], &m.mainName); err != nil {
				return fmt.Errorf("decode mainObjectName: %w", err)
			}
			if m.mainName == nil {
				return errors.New("mainObjectName: no answer")
			}
			delete(m.pending, "mainObjectName")
		}
		return nil
	}
}

// missing lists the pending lookups, sorted, for an error.
func (m *ModuleNaming) missing() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var missing []string
	for field, names := range m.pending {
		for name := range names {
			missing = append(missing, fmt.Sprintf("%q (%s)", name, field))
		}
	}
	for _, candidates := range m.pendingMains {
		missing = append(missing, fmt.Sprintf("mainObject%q", candidates))
	}
	slices.Sort(missing)
	return missing
}
