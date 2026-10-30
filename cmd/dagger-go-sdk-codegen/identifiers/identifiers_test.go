package identifiers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

func TestApplies(t *testing.T) {
	for version, want := range map[string]bool{
		"":                                false,
		"not-a-version":                   false,
		"v0.21.9":                         false,
		"v1.0.0-0":                        false,
		"v1.0.0-beta.20":                  false,
		"v1.0.0-beta.21":                  false,
		"v1.0.0-beta.21+abc123":           false,
		"v1.0.0-beta.22":                  true,
		"1.0.0-beta.22":                   true,
		"v1.0.0-beta.22+441d95c2":         true,
		"v1.0.0-beta.23":                  true,
		"v1.0.0-rc.1":                     true,
		"v1.0.0":                          true,
		"v1.0.0-dev-0123456789ab":         true, // a dev build counts as its release
		"v1.0.0-beta.22-dev-0123456789ab": true,
		"v1.1.0":                          true,
	} {
		require.Equal(t, want, identifiers.Applies(version), version)
	}
}

var table = identifierstest.Table{
	"PASCAL/UPPERCASE": {
		"commitSha": "CommitSHA",
		"shortSha":  "ShortSHA",
		"JSONValue": "JSONValue",
	},
	"CAMEL/CAPITALIZED": {
		"HTTPClient": "httpClient",
		"GetURL":     "getUrl",
	},
}

// Before the cut, nothing connects: the legacy rules need no engine.
func TestConnectorLegacySchemaDoesNotConnect(t *testing.T) {
	c := identifiers.NewConnector(func() (identifiers.Engine, error) {
		return nil, errors.New("must not connect")
	})
	for _, version := range []string{"", "v0.21.9", "v1.0.0-beta.21"} {
		names, err := c.Names(t.Context(), version)
		require.NoError(t, err)
		require.Nil(t, names)
	}
}

// Engines before the cut name modules by strcase, and report every v1.0.0
// prerelease view as v1.0.0: the schema's version alone can't tell.
func TestConnectorOlderEngineKeepsLegacyRules(t *testing.T) {
	for _, release := range []string{"v1.0.0-beta.19", "v1.0.0-beta.21+abc", "v0.21.9"} {
		engine := &identifierstest.Engine{Release: release, Table: table}
		names, err := engine.Connector().Names(t.Context(), "v1.0.0")
		require.NoError(t, err)
		require.Nil(t, names, release)
		require.Equal(t, 0, engine.FormatQueries())
	}
}

func TestConnectorAppliesPastTheCut(t *testing.T) {
	for _, release := range []string{"v1.0.0-beta.22", "v1.0.0-beta.22+441d95c2", "v1.0.0-dev-0123456789ab"} {
		engine := &identifierstest.Engine{Release: release, Table: table}
		c := engine.Connector()
		names, err := c.Names(t.Context(), "v1.0.0-beta.22")
		require.NoError(t, err)
		require.NotNil(t, names, release)
		require.Equal(t, "v1.0.0-beta.22", names.Version())
		again, err := c.Names(t.Context(), "v1.0.0-beta.22")
		require.NoError(t, err)
		require.Same(t, names, again, "a version has one Names")
		require.Len(t, engine.Queries(), 1, "the engine's release is asked once")
	}
}

// A schema past the cut can't be generated without the engine: silently
// keeping the legacy rules would produce bindings the engine doesn't match.
func TestConnectorPastTheCutNeedsAnEngine(t *testing.T) {
	t.Setenv("DAGGER_SESSION_PORT", "")
	t.Setenv("DAGGER_SESSION_TOKEN", "")
	_, err := identifiers.SessionConnector().Names(t.Context(), "v1.0.0-beta.22")
	require.ErrorContains(t, err, "DAGGER_SESSION_PORT")
}

func TestNilNamesAreTheLegacyRules(t *testing.T) {
	var names *identifiers.Names
	_, ok := names.Lookup(identifiers.GoName, "commitSha")
	require.False(t, ok)
	require.NoError(t, names.Prefetch(map[identifiers.Format][]string{identifiers.GoName: {"commitSha"}}))
	require.NoError(t, names.Err())
	require.Empty(t, names.Version())
}

// Prefetch formats every name it's given in one query, one aliased field per
// format, each name once; lookups of them need no other query.
func TestPrefetch(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
	names := identifiers.NewNames(t.Context(), engine, "v1.0.0-beta.22")
	require.NoError(t, names.Prefetch(map[identifiers.Format][]string{
		identifiers.GoName:    {"commitSha", "shortSha", "commitSha", "JSONValue"},
		identifiers.FieldName: {"HTTPClient", "GetURL"},
	}))
	require.Equal(t, 1, engine.FormatQueries())
	query := engine.Queries()[0]
	require.Contains(t, query, "query FormatIdentifiers($v0: String!, $n0: [String!]!, $n1: [String!]!)")
	require.Contains(t, query, "f0: formatIdentifiers(names: $n0, casing: CAMEL, acronyms: CAPITALIZED, version: $v0)")
	require.Contains(t, query, "f1: formatIdentifiers(names: $n1, casing: PASCAL, acronyms: UPPERCASE, version: $v0)")

	var got []string
	for _, lookup := range []struct {
		f    identifiers.Format
		name string
	}{
		{identifiers.GoName, "commitSha"},
		{identifiers.TypeName, "JSONValue"},
		{identifiers.FieldName, "GetURL"},
	} {
		formatted, ok := names.Lookup(lookup.f, lookup.name)
		require.True(t, ok, lookup.name)
		got = append(got, formatted)
	}
	require.Equal(t, []string{"CommitSHA", "JSONValue", "getUrl"}, got)
	require.Equal(t, 1, engine.FormatQueries())

	// Prefetching formatted names again sends nothing.
	require.NoError(t, names.Prefetch(map[identifiers.Format][]string{identifiers.GoName: {"commitSha"}}))
	require.Equal(t, 1, engine.FormatQueries())
}

// A name that wasn't prefetched, such as one a generator derives while
// rendering, is formatted with a direct call when first looked up, and kept.
func TestLookupFormatsMissesDirectly(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
	names := identifiers.NewNames(t.Context(), engine, "v1.0.0-beta.22")
	for range 2 {
		got, ok := names.Lookup(identifiers.GoName, "shortSha")
		require.True(t, ok)
		require.Equal(t, "ShortSHA", got)
	}
	require.Equal(t, 1, engine.FormatQueries())
	require.NoError(t, names.Err())
}

// A name the engine can't format fails the call: Prefetch returns the error,
// and a Lookup keeps it for Err, so generation fails.
func TestFormatErrorsFailGeneration(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
	names := identifiers.NewNames(t.Context(), engine, "v1.0.0-beta.22")
	err := names.Prefetch(map[identifiers.Format][]string{identifiers.GoName: {"commitSha", "naïve"}})
	require.ErrorContains(t, err, `no PASCAL/UPPERCASE formatting of "naïve"`)

	_, ok := names.Lookup(identifiers.GoName, "naïve")
	require.False(t, ok)
	require.ErrorContains(t, names.Err(), `no PASCAL/UPPERCASE formatting of "naïve"`)
	// The first failure is the one returned; later misses don't call again.
	queries := engine.FormatQueries()
	_, ok = names.Lookup(identifiers.GoName, "shortSha")
	require.False(t, ok)
	require.Equal(t, queries, engine.FormatQueries())
}

// Session speaks the engine's GraphQL HTTP protocol: POST /query, the
// session token as the basic auth user.
func TestSession(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22+441d95c2", Table: table}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != http.MethodPost || r.URL.Path != "/query" || !ok || user != "secret" || pass != "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// JSON decodes lists as []any; the fake wants []string.
		for k, v := range req.Variables {
			if list, ok := v.([]any); ok {
				strs := make([]string, len(list))
				for i, s := range list {
					strs[i] = s.(string)
				}
				req.Variables[k] = strs
			}
		}
		var data map[string]any
		if err := engine.Query(r.Context(), req.Query, req.Variables, &data); err != nil {
			json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": err.Error()}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	t.Setenv("DAGGER_SESSION_PORT", strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	t.Setenv("DAGGER_SESSION_TOKEN", "secret")

	names, err := identifiers.SessionConnector().Names(context.Background(), "v1.0.0-beta.22")
	require.NoError(t, err)
	require.NotNil(t, names)
	got, ok := names.Lookup(identifiers.GoName, "commitSha")
	require.True(t, ok)
	require.Equal(t, "CommitSHA", got)

	err = names.Prefetch(map[identifiers.Format][]string{identifiers.GoName: {"unknownName"}})
	require.ErrorContains(t, err, `no PASCAL/UPPERCASE formatting of "unknownName"`)
}
