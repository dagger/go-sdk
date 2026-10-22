package introspection

import (
	"context"
	"fmt"

	"dagger.io/dagger"
)

// Introspect gets the Dagger schema from the engine that dag is connected to.
func Introspect(ctx context.Context, dag *dagger.Client) (*Schema, string, error) {
	var introspectionResp Response
	err := dag.Do(ctx, &dagger.Request{
		Query:  Query,
		OpName: "IntrospectionQuery",
	}, &dagger.Response{
		Data: &introspectionResp,
	})
	if err != nil {
		return nil, "", fmt.Errorf("introspection query: %w", err)
	}

	return introspectionResp.Schema, introspectionResp.SchemaVersion, nil
}

// EngineVersion gets the version of the engine that dag is connected to
// (Query.version), which decides whether its schema names identifiers by the
// engine's rules (see Schema.NamesIdentifiers).
func EngineVersion(ctx context.Context, dag *dagger.Client) (string, error) {
	var data struct {
		Version string `json:"version"`
	}
	if err := dag.Do(ctx, &dagger.Request{
		Query:  "query EngineVersion { version }",
		OpName: "EngineVersion",
	}, &dagger.Response{Data: &data}); err != nil {
		return "", fmt.Errorf("engine version query: %w", err)
	}
	return data.Version, nil
}
