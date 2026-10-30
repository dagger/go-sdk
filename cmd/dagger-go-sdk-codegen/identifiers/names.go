package identifiers

import (
	"context"
	"fmt"
	"sync"
)

// Names are Go identifiers formatted by the engine (Query.formatIdentifiers:
// PascalCase, uppercase acronyms) with the dictionary of one version.
// Prefetch formats the names a generator knows it will look up, in one
// query; Lookup finds them, and formats any other name with a direct call.
type Names struct {
	// ctx is the generation's: Lookup is called while rendering, which has
	// none of its own.
	ctx     context.Context
	query   Query
	version string

	mu        sync.Mutex
	formatted map[string]string
	// err is the first failure of a Lookup's direct call.
	err error
}

// NewNames returns Names that format names with the dictionary of version
// (a schema's __schemaVersion, or the version a runtime's identifiers
// follow), through query, within ctx.
func NewNames(ctx context.Context, query Query, version string) *Names {
	return &Names{ctx: ctx, query: query, version: version, formatted: map[string]string{}}
}

// Prefetch formats names in one query and keeps them for Lookup. A name the
// engine can't parse fails the query.
func (n *Names) Prefetch(names []string) error {
	if len(names) == 0 {
		return nil
	}
	formatted, err := n.format(names)
	if err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, name := range names {
		n.formatted[name] = formatted[i]
	}
	return nil
}

// Lookup returns name formatted: a prefetched name from memory, any other
// with a direct call. When that call fails, Lookup returns false and keeps
// the error for Err; the caller's fallback stands in until the generator
// returns it.
func (n *Names) Lookup(name string) (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if formatted, ok := n.formatted[name]; ok {
		return formatted, true
	}
	if n.err != nil {
		return "", false
	}
	formatted, err := n.format([]string{name})
	if err != nil {
		n.err = err
		return "", false
	}
	n.formatted[name] = formatted[0]
	return formatted[0], true
}

// Err returns the first failure of a Lookup: a generator that looked names
// up returns it, since what it rendered used a fallback name.
func (n *Names) Err() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.err
}

func (n *Names) format(names []string) ([]string, error) {
	var data struct {
		Names []string `json:"formatIdentifiers"`
	}
	err := n.query(n.ctx, `query FormatIdentifiers($names: [String!]!, $version: String!) {
  formatIdentifiers(names: $names, casing: PASCAL, acronyms: UPPERCASE, version: $version)
}`, map[string]any{"names": names, "version": n.version}, &data)
	if err != nil {
		return nil, fmt.Errorf("format Go identifiers: %w", err)
	}
	if len(data.Names) != len(names) {
		return nil, fmt.Errorf("format Go identifiers: got %d names for %d", len(data.Names), len(names))
	}
	return data.Names, nil
}
