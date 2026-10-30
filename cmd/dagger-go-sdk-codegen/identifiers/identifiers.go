// Package identifiers names identifiers through the engine, so generators
// don't carry a copy of its naming rules: Go identifiers by
// Query.formatIdentifiers (Names), and the schema names the engine gives a
// module's own declarations by ModuleSource.naming (ModuleNames,
// dagger/dagger#14996). Both ask the Dagger session the generator runs in; a
// name the engine can't parse fails generation.
package identifiers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Query runs a GraphQL query against the engine and decodes the response's
// data into data. GraphQL errors are returned as an error.
type Query func(ctx context.Context, query string, variables map[string]any, data any) error

// SessionQuery returns the Query of the Dagger session a command runs in as
// a nested client: DAGGER_SESSION_PORT and DAGGER_SESSION_TOKEN.
func SessionQuery() (Query, error) {
	port, token := os.Getenv("DAGGER_SESSION_PORT"), os.Getenv("DAGGER_SESSION_TOKEN")
	if port == "" || token == "" {
		return nil, errors.New("no Dagger session to name identifiers: DAGGER_SESSION_PORT and DAGGER_SESSION_TOKEN are not set; run the command as a nested client of a Dagger session")
	}
	return func(ctx context.Context, query string, variables map[string]any, data any) error {
		body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+"/query", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(token, "")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var result struct {
			Data   json.RawMessage
			Errors []struct{ Message string }
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("decode response (HTTP %s): %w", resp.Status, err)
		}
		if len(result.Errors) > 0 {
			messages := make([]string, len(result.Errors))
			for i, e := range result.Errors {
				messages[i] = e.Message
			}
			return errors.New(strings.Join(messages, "; "))
		}
		return json.Unmarshal(result.Data, data)
	}, nil
}
