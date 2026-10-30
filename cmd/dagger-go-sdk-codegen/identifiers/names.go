package identifiers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Names are names formatted by the engine's naming rules of one version,
// looked up while a generator renders, and formatted in batches by Run.
//
// A nil *Names is the legacy rules: Lookup finds nothing and Run renders
// once, so a generator falls back to its legacy conversions everywhere.
type Names struct {
	engine  Engine
	version string

	mu sync.Mutex
	// formatted holds the formatted names, by format and name.
	formatted map[Format]map[string]string
	// pending holds the names looked up but not formatted yet.
	pending map[Format]map[string]bool
}

// NewNames returns empty Names that formats names with the dictionary of
// version, through engine. Most generators get theirs from a Connector, which
// applies the gate first.
func NewNames(engine Engine, version string) *Names {
	return &Names{
		engine:    engine,
		version:   version,
		formatted: map[Format]map[string]string{},
		pending:   map[Format]map[string]bool{},
	}
}

// Version is the version whose dictionary names are parsed with, or "" for
// the legacy rules.
func (n *Names) Version() string {
	if n == nil {
		return ""
	}
	return n.version
}

// Lookup returns name formatted in f. When it isn't formatted yet, Lookup
// records it for the next batch and returns false, and so it does for a name
// the engine can't parse (see Valid), which no batch formats: either way the
// caller uses its fallback. A nil *Names always returns false.
func (n *Names) Lookup(f Format, name string) (string, bool) {
	if n == nil || !Valid(name) {
		return "", false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if formatted, ok := n.formatted[f][name]; ok {
		return formatted, true
	}
	if n.pending[f] == nil {
		n.pending[f] = map[string]bool{}
	}
	n.pending[f][name] = true
	return "", false
}

// Prefetch records names to format in the next batch, so that the first
// render can find them.
func (n *Names) Prefetch(f Format, names ...string) {
	for _, name := range names {
		n.Lookup(f, name)
	}
}

// maxRounds bounds Run. A render only looks up names derived from what it
// rendered before, so it settles in a round or two; more means a bug.
const maxRounds = 4

// Run calls render until every name it looks up is formatted. Before each
// render, the names recorded since the last batch are formatted in one
// round trip; a render that recorded nothing new is final. A render that
// fails while names were still missing is retried with them formatted, since
// a fallback name may be what it failed on.
//
// With a nil *Names, Run calls render once.
func (n *Names) Run(ctx context.Context, render func() error) error {
	return Run(ctx, render, n)
}

// A Batch holds names a generator looks up while it renders, which Run
// answers in batches: *Names (formatIdentifiers) or *ModuleNaming
// (ModuleSource.naming). A nil one answers nothing.
type Batch interface {
	// session is the engine the batch asks; nil for a nil batch.
	session() Engine
	// add adds the pending lookups to q, and returns how to store their
	// answers; nil when nothing is pending.
	add(q *query) func(data map[string]json.RawMessage) error
	// missing lists the pending lookups, sorted, for an error.
	missing() []string
}

// Run is Names.Run for a render that looks names up in several batches, such
// as a module's bindings, whose Go identifiers follow one version and whose
// schema names come from the module's naming. Each round answers the lookups
// missing from all of them in a single round trip. Nil batches are the legacy
// rules and are skipped; with no others, render runs once.
func Run(ctx context.Context, render func() error, batches ...Batch) error {
	var all []Batch
	for _, b := range batches {
		if b != nil && b.session() != nil && !slices.Contains(all, b) {
			all = append(all, b)
		}
	}
	if len(all) == 0 {
		return render()
	}
	for round := 1; ; round++ {
		if err := fetch(ctx, all); err != nil {
			return err
		}
		err := render()
		var missing []string
		for _, b := range all {
			missing = append(missing, b.missing()...)
		}
		switch {
		case len(missing) == 0:
			return err
		case round == maxRounds:
			if err != nil {
				return err
			}
			return fmt.Errorf("identifiers: names still unformatted after %d rounds: %s", maxRounds, strings.Join(missing, ", "))
		}
	}
}

// query is a batch query being built: its variables and fields.
type query struct {
	params []string
	fields strings.Builder
	vars   map[string]any
	counts map[string]int
}

// param declares a variable of type typ holding value, named prefix and a
// number, and returns its name.
func (q *query) param(prefix, typ string, value any) string {
	name := fmt.Sprintf("%s%d", prefix, q.counts[prefix])
	q.counts[prefix]++
	q.params = append(q.params, "$"+name+": "+typ)
	q.vars[name] = value
	return name
}

// fetch answers the pending lookups of every batch in a single query. Every
// batch comes from the same engine.
func fetch(ctx context.Context, all []Batch) error {
	q := &query{vars: map[string]any{}, counts: map[string]int{}}
	var stores []func(map[string]json.RawMessage) error
	for _, b := range all {
		if store := b.add(q); store != nil {
			stores = append(stores, store)
		}
	}
	if len(stores) == 0 {
		return nil
	}
	text := fmt.Sprintf("query FormatIdentifiers(%s) {\n%s}", strings.Join(q.params, ", "), q.fields.String())
	var data map[string]json.RawMessage
	if err := all[0].session().Query(ctx, text, q.vars, &data); err != nil {
		return fmt.Errorf("format identifiers: %w", err)
	}
	for _, store := range stores {
		if err := store(data); err != nil {
			return fmt.Errorf("format identifiers: %w", err)
		}
	}
	return nil
}

func (n *Names) session() Engine {
	if n == nil {
		return nil
	}
	return n.engine
}

// missing lists the pending names, sorted, for an error.
func (n *Names) missing() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var missing []string
	for f, names := range n.pending {
		for name := range names {
			missing = append(missing, fmt.Sprintf("%q (%s, %s)", name, f.Casing, n.version))
		}
	}
	slices.Sort(missing)
	return missing
}

// request is one formatIdentifiers call of a batch.
type request struct {
	format Format
	names  []string
}

// add adds an aliased formatIdentifiers field per format with pending names.
func (n *Names) add(q *query) func(map[string]json.RawMessage) error {
	requests := n.requests()
	if len(requests) == 0 {
		return nil
	}
	version := q.param("v", "String!", n.version)
	aliases := make([]string, len(requests))
	for i, r := range requests {
		names := q.param("n", "[String!]!", r.names)
		aliases[i] = "f" + strings.TrimPrefix(names, "n")
		fmt.Fprintf(&q.fields, "  %s: formatIdentifiers(names: $%s, casing: %s, acronyms: %s, version: $%s)\n",
			aliases[i], names, r.format.Casing, r.format.Acronyms, version)
	}
	return func(data map[string]json.RawMessage) error {
		for i, r := range requests {
			var formatted []string
			if err := json.Unmarshal(data[aliases[i]], &formatted); err != nil {
				return fmt.Errorf("decode %s: %w", aliases[i], err)
			}
			if len(formatted) != len(r.names) {
				return fmt.Errorf("got %d names for %d", len(formatted), len(r.names))
			}
			n.store(r, formatted)
		}
		return nil
	}
}

// requests returns the pending names as requests, one per format, sorted.
func (n *Names) requests() []request {
	n.mu.Lock()
	defer n.mu.Unlock()
	var requests []request
	for f, names := range n.pending {
		if len(names) == 0 {
			continue
		}
		requests = append(requests, request{format: f, names: slices.Sorted(maps.Keys(names))})
	}
	slices.SortFunc(requests, func(a, b request) int {
		return strings.Compare(string(a.format.Casing)+" "+string(a.format.Acronyms), string(b.format.Casing)+" "+string(b.format.Acronyms))
	})
	return requests
}

// store records the formatted names of a request.
func (n *Names) store(r request, formatted []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.formatted[r.format] == nil {
		n.formatted[r.format] = map[string]string{}
	}
	for i, name := range r.names {
		n.formatted[r.format][name] = formatted[i]
		delete(n.pending[r.format], name)
	}
}
