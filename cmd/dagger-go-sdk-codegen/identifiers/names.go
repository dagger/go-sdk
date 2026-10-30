package identifiers

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Names are names formatted by the engine's naming rules of one version.
// Prefetch formats the names a generator knows it will look up, in one
// query; Lookup finds them, and formats any other name with a direct call,
// which it caches.
//
// A nil *Names is the legacy rules: Lookup finds nothing, so a generator
// falls back to its legacy conversions everywhere.
type Names struct {
	// ctx is the generation's: Lookup is called while rendering, which
	// has none of its own.
	ctx     context.Context
	engine  Engine
	version string

	mu sync.Mutex
	// formatted holds the formatted names, by format and name.
	formatted map[Format]map[string]string
	// err is the first formatIdentifiers failure of a Lookup.
	err error
}

// NewNames returns empty Names that formats names with the dictionary of
// version, through engine, within ctx. Most generators get theirs from a
// Connector, which applies the gate first.
func NewNames(ctx context.Context, engine Engine, version string) *Names {
	return &Names{
		ctx:       ctx,
		engine:    engine,
		version:   version,
		formatted: map[Format]map[string]string{},
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

// Prefetch formats names, by format, in a single formatIdentifiers query
// (one aliased field per format), and keeps them for Lookup. Names already
// formatted aren't sent again. A name the engine can't parse fails the
// query, and Prefetch returns that error.
func (n *Names) Prefetch(names map[Format][]string) error {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	var requests []request
	for _, f := range sortedFormats(names) {
		var missing []string
		for _, name := range names[f] {
			if _, ok := n.formatted[f][name]; !ok && !slices.Contains(missing, name) {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			slices.Sort(missing)
			requests = append(requests, request{format: f, version: n.version, names: missing})
		}
	}
	n.mu.Unlock()
	if len(requests) == 0 {
		return nil
	}
	formatted, err := formatIdentifiers(n.ctx, n.engine, requests)
	if err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, r := range requests {
		n.store(r.format, r.names, formatted[i])
	}
	return nil
}

// Lookup returns name formatted in f: a prefetched name from memory, any
// other with a direct formatIdentifiers call, whose result it keeps. When
// that call fails, Lookup returns false and keeps the error for Err; the
// caller's fallback stands in until the generator returns it. A nil *Names
// always returns false.
func (n *Names) Lookup(f Format, name string) (string, bool) {
	if n == nil {
		return "", false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if formatted, ok := n.formatted[f][name]; ok {
		return formatted, true
	}
	if n.err != nil {
		return "", false
	}
	formatted, err := formatIdentifiers(n.ctx, n.engine, []request{{format: f, version: n.version, names: []string{name}}})
	if err != nil {
		n.err = err
		return "", false
	}
	n.store(f, []string{name}, formatted[0])
	return formatted[0][0], true
}

// Err returns the first failure of a Lookup's formatIdentifiers call: a
// generator that looked names up returns it, since what it rendered used a
// fallback name.
func (n *Names) Err() error {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.err
}

// store keeps formatted names; the caller holds n.mu.
func (n *Names) store(f Format, names, formatted []string) {
	if n.formatted[f] == nil {
		n.formatted[f] = map[string]string{}
	}
	for i, name := range names {
		n.formatted[f][name] = formatted[i]
	}
}

// sortedFormats returns the formats of names in a stable order, so a
// query's aliases don't depend on map order.
func sortedFormats(names map[Format][]string) []Format {
	return slices.SortedFunc(maps.Keys(names), func(a, b Format) int {
		return strings.Compare(string(a.Casing)+" "+string(a.Acronyms), string(b.Casing)+" "+string(b.Acronyms))
	})
}
