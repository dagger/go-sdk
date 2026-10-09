package gogenerator

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

func TestGenerateEmbeddedClientUsesModuleBootstrap(t *testing.T) {
	gen := &GoGenerator{Config: generator.Config{
		OutputDir: t.TempDir(),
		ModuleConfig: &generator.ModuleGeneratorConfig{
			ModuleName: "hello",
		},
	}}
	state, err := gen.GenerateEmbeddedClient(
		t.Context(),
		buildClientSchema(),
		"v1.0.0-beta.11",
		"example.com/hello/internal/dagger",
	)
	require.NoError(t, err)

	generated, err := fs.ReadFile(state.Overlay, "internal/dagger/dagger.gen.go")
	require.NoError(t, err)
	source := string(generated)
	require.Equal(t, 1, strings.Count(source, "type Client struct"))
	require.Contains(t, source, "func Connect() *Client")
	require.NotContains(t, source, "func Connect(ctx context.Context")
	require.Contains(t, source, "unavailableGraphQLClient")

	_, err = fs.Stat(state.Overlay, "go.mod")
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = fs.Stat(state.Overlay, "internal/dagger/internal")
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestEmbeddedClientSharesSuccessfulSessionWithoutGlobalAPI(t *testing.T) {
	sdk := os.Getenv("GO_SDK_TEST_RUNTIME")
	if sdk == "" {
		t.Skip("set GO_SDK_TEST_RUNTIME to the SDK split checkout")
	}
	root := t.TempDir()
	gen := &GoGenerator{Config: generator.Config{
		OutputDir:    root,
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "hello"},
	}}
	data, err := os.ReadFile(filepath.Join("testdata", "core", "schema.json"))
	require.NoError(t, err)
	var resp introspection.Response
	require.NoError(t, json.Unmarshal(data, &resp))
	generator.SetSchemaParents(resp.Schema)
	state, err := gen.GenerateEmbeddedClient(t.Context(), resp.Schema, resp.SchemaVersion, "example.com/hello/internal/dagger")
	require.NoError(t, err)
	require.NoError(t, generator.Overlay(t.Context(), state.Overlay, root))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hello\n\ngo 1.26\n\nrequire dagger.io/dagger v1.0.0-beta.15\nreplace dagger.io/dagger => "+sdk+"\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal/dagger/connect_test.go"), []byte(`package dagger
import "testing"
func TestSharedSession(t *testing.T) {
 t.Setenv("DAGGER_SESSION_PORT", "invalid")
 t.Setenv("DAGGER_SESSION_TOKEN", "")
 if _, ok := Connect().GraphQLClient().(unavailableGraphQLClient); !ok {
  t.Fatal("an unattached handle should defer a session error")
 }
 if defaultGraphQLClient.client != nil {
  t.Fatal("an unavailable session must not poison the default transport")
 }
 t.Setenv("DAGGER_SESSION_PORT", "12345")
 t.Setenv("DAGGER_SESSION_TOKEN", "fixture-token")
 first := Connect()
 clients := make(chan *Client, 16)
 for range 16 { go func() { clients <- Connect() }() }
 for range 16 {
  next := <-clients
  if first.GraphQLClient() != next.GraphQLClient() {
   t.Fatal("Connect must share its transport")
  }
  if first == next || first.QueryBuilder() == next.QueryBuilder() {
   t.Fatal("Connect must return an independent API handle and query root")
  }
 }
}
`), 0644))
	cmd := exec.CommandContext(t.Context(), "go", "test", "-race", "-mod=mod", "-buildvcs=false", "./internal/dagger")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
