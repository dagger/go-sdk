package identifiers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

// A name that wasn't prefetched is formatted with a direct call when first
// looked up, and kept. The first failure is kept for Err, and later misses
// don't call again.
func TestLookup(t *testing.T) {
	calls := 0
	fail := false
	query := func(ctx context.Context, q string, variables map[string]any, data any) error {
		calls++
		if fail {
			return errors.New("boom")
		}
		return identifierstest.Query(ctx, q, variables, data)
	}
	names := identifiers.NewNames(t.Context(), query, "v1.0.0-beta.22")
	require.NoError(t, names.Prefetch([]string{"commitSha"}))
	for range 2 {
		for name, want := range map[string]string{"commitSha": "CommitSHA", "shortSha": "ShortSHA"} {
			got, ok := names.Lookup(name)
			require.True(t, ok)
			require.Equal(t, want, got)
		}
	}
	require.Equal(t, 2, calls)
	require.NoError(t, names.Err())

	fail = true
	for range 2 {
		_, ok := names.Lookup("withGpu")
		require.False(t, ok)
	}
	require.Equal(t, 3, calls)
	require.ErrorContains(t, names.Err(), "boom")
}
