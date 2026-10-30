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
	"sync"
)

// Engine runs GraphQL queries against the Dagger engine.
type Engine interface {
	// Query runs query with variables and decodes the response's data into
	// data. GraphQL errors are returned as an error.
	Query(ctx context.Context, query string, variables map[string]any, data any) error
}

// Connector applies the gate: it hands out Names for a version only when the
// version and the engine are both past CasingVersion. It connects to the
// engine, and asks for its release, at most once, and only when something
// needs it.
type Connector struct {
	dial func() (Engine, error)

	dialOnce sync.Once
	engine   Engine
	dialErr  error

	releaseOnce sync.Once
	release     string
	releaseErr  error

	mu    sync.Mutex
	names map[string]*Names
}

// NewConnector returns a Connector that connects with dial.
func NewConnector(dial func() (Engine, error)) *Connector {
	return &Connector{dial: dial, names: map[string]*Names{}}
}

// SessionConnector returns a Connector to the session of the environment
// (see SessionFromEnv).
func SessionConnector() *Connector {
	return NewConnector(func() (Engine, error) { return SessionFromEnv() })
}

// Names returns the Names for code generated at version: a schema's
// __schemaVersion, or the version a runtime's Go identifiers follow. It is
// nil, the legacy rules, when version is before CasingVersion, without
// connecting, or when the engine's release is. Each version has one Names,
// so everything generated at a version shares its formatted names.
func (c *Connector) Names(ctx context.Context, version string) (*Names, error) {
	if !Applies(version) {
		return nil, nil
	}
	release, err := c.engineRelease(ctx)
	if err != nil {
		return nil, fmt.Errorf("naming identifiers at %s depends on the engine's release: %w", version, err)
	}
	if !Applies(release) {
		// An older engine names every module by strcase, and its schemas
		// report every v1.0.0 prerelease as v1.0.0, past CasingVersion.
		return nil, nil
	}
	return c.namesAt(version), nil
}

// namesAt returns the one Names of version.
func (c *Connector) namesAt(version string) *Names {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.names[version] == nil {
		c.names[version] = NewNames(c.engine, version)
	}
	return c.names[version]
}

// connect connects to the engine, once.
func (c *Connector) connect() (Engine, error) {
	c.dialOnce.Do(func() {
		c.engine, c.dialErr = c.dial()
		if c.dialErr != nil {
			c.dialErr = fmt.Errorf("connect to the engine to name identifiers: %w", c.dialErr)
		}
	})
	return c.engine, c.dialErr
}

// engineRelease connects to the engine and returns its release:
// Query.version.
func (c *Connector) engineRelease(ctx context.Context) (string, error) {
	engine, err := c.connect()
	if err != nil {
		return "", err
	}
	c.releaseOnce.Do(func() {
		var data struct {
			Version string `json:"version"`
		}
		if err := engine.Query(ctx, "query EngineVersion { version }", nil, &data); err != nil {
			c.releaseErr = fmt.Errorf("query engine version: %w", err)
		}
		c.release = data.Version
	})
	return c.release, c.releaseErr
}

// Session is the Dagger session a command runs in: the engine's GraphQL
// endpoint on localhost, which the engine provides to a command it runs as a
// nested client (withExec with experimentalPrivilegedNesting, the default in
// the v1 API).
type Session struct {
	url    string
	token  string
	client *http.Client
}

// SessionFromEnv returns the session of DAGGER_SESSION_PORT and
// DAGGER_SESSION_TOKEN.
func SessionFromEnv() (*Session, error) {
	port := os.Getenv("DAGGER_SESSION_PORT")
	token := os.Getenv("DAGGER_SESSION_TOKEN")
	if port == "" || token == "" {
		return nil, errors.New("no Dagger session: DAGGER_SESSION_PORT and DAGGER_SESSION_TOKEN are not set; run the command as a nested client of a Dagger session")
	}
	return &Session{url: "http://127.0.0.1:" + port + "/query", token: token, client: http.DefaultClient}, nil
}

// Query implements Engine.
func (s *Session) Query(ctx context.Context, query string, variables map[string]any, data any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(s.token, "")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return json.Unmarshal(result.Data, data)
}
