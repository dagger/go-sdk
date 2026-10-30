package identifiers

import (
	"context"
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
	if n == nil {
		return render()
	}
	for round := 1; ; round++ {
		if err := n.format(ctx); err != nil {
			return err
		}
		err := render()
		missing := n.missing()
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

// missing lists the pending names, sorted, for an error.
func (n *Names) missing() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var missing []string
	for f, names := range n.pending {
		for name := range names {
			missing = append(missing, fmt.Sprintf("%q (%s)", name, f.Casing))
		}
	}
	slices.Sort(missing)
	return missing
}

// format formats the pending names in a single query: one aliased
// formatIdentifiers field per format, every one at n.version.
func (n *Names) format(ctx context.Context) error {
	n.mu.Lock()
	var requests []request
	for f, names := range n.pending {
		if len(names) == 0 {
			continue
		}
		requests = append(requests, request{format: f, names: slices.Sorted(maps.Keys(names))})
	}
	n.mu.Unlock()
	if len(requests) == 0 {
		return nil
	}
	slices.SortFunc(requests, func(a, b request) int {
		return strings.Compare(string(a.format.Casing)+" "+string(a.format.Acronyms), string(b.format.Casing)+" "+string(b.format.Acronyms))
	})
	formatted, err := formatIdentifiers(ctx, n.engine, n.version, requests)
	if err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, r := range requests {
		if n.formatted[r.format] == nil {
			n.formatted[r.format] = map[string]string{}
		}
		for j, name := range r.names {
			n.formatted[r.format][name] = formatted[i][j]
			delete(n.pending[r.format], name)
		}
	}
	return nil
}
