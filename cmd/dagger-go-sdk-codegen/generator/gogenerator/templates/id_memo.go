package templates

// IDMemoHelper is the Go source of memoizedID and its state, which generated
// ID() methods use to remember the ID fetched for each object. Every generated
// package that declares object types includes it: defs.go.tmpl, and the
// unified client's own dagger.gen.go.
const IDMemoHelper = `// idMemo holds the ID fetched for one object query.
type idMemo struct {
	mu       sync.Mutex
	id       string
	ok       bool
	fetching chan struct{} // closed when the fetch in flight ends
}

// idMemos maps an object's query to its idMemo. Keys are weak pointers, and an
// entry is deleted once its query is garbage collected.
var idMemos sync.Map // weak.Pointer[querybuilder.Selection] -> *idMemo

// memoizedID returns the ID of the object built by query q, calling fetch at
// most once per q, so an object that is passed as an argument many times
// fetches its ID once. Concurrent callers wait for the fetch in flight, or
// until their own ctx is done. A failed fetch is not remembered: the next
// caller fetches again.
func memoizedID[T ~string](ctx context.Context, q *querybuilder.Selection, fetch func() (T, error)) (T, error) {
	key := weak.Make(q)
	v, loaded := idMemos.LoadOrStore(key, &idMemo{})
	if !loaded {
		runtime.AddCleanup(q, func(key weak.Pointer[querybuilder.Selection]) {
			idMemos.Delete(key)
		}, key)
	}
	m := v.(*idMemo)
	for {
		m.mu.Lock()
		if m.ok {
			m.mu.Unlock()
			return T(m.id), nil
		}
		if m.fetching == nil {
			break
		}
		fetching := m.fetching
		m.mu.Unlock()
		select {
		case <-fetching:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	fetching := make(chan struct{})
	m.fetching = fetching
	m.mu.Unlock()

	id, err := fetch()

	m.mu.Lock()
	if err == nil {
		m.id, m.ok = string(id), true
	}
	m.fetching = nil
	m.mu.Unlock()
	close(fetching)
	return id, err
}
`
