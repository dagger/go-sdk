package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vito/dang/v2/pkg/dang"
)

const validEntrypoint = `type Entrypoint implements ModuleEntrypoint {
  pub sources(workspace: Workspace!): Directory! { directory }
  pub types: [TypeDef!]! { [] }
  pub call(
    receiverType: String!,
    receiverValue: JSON,
    fnName: String!,
    fnArgs: JSON!,
    callDigest: String,
  ): JSON! {
    ("null" :: JSON!)
  }
}
`

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.dang"), []byte(validEntrypoint), 0o600))

	err := Check(context.Background(), dir, filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json"), dang.Version{})
	require.NoError(t, err)
}

// The entrypoint is checked at the Dang version its dang.toml declares, as
// the engine evaluates it: Dang 2.1 accepts a bare type pattern, which later
// versions refuse.
func TestCheckUsesTheDeclaredVersion(t *testing.T) {
	dir := t.TempDir()
	entrypoint := strings.Replace(validEntrypoint, `("null" :: JSON!)`, "let result = directory.file(\"result.json\").contents rescue {\n      err: GraphQLError => raise err\n    }\n    (result :: JSON!)", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.dang"), []byte(entrypoint), 0o600))
	schema := filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json")

	v21, err := dang.ParseVersion("2.1")
	require.NoError(t, err)
	require.NoError(t, Check(context.Background(), dir, schema, v21))
	require.ErrorContains(t, Check(context.Background(), dir, schema, dang.Version{}), "a type pattern is the non-null type its arm binds")
}

// An entrypoint generated for engines that pass a workspace to types and
// call does not satisfy the contract of an entrypoint with sources.
func TestCheckRejectsRequiredWorkspace(t *testing.T) {
	dir := t.TempDir()
	old := `type Entrypoint implements ModuleEntrypoint {
  pub sources(workspace: Workspace!): Directory! { directory }
  pub types(workspace: Workspace!): [TypeDef!]! { [] }
  pub call(
    workspace: Workspace!,
    receiverType: String!,
    receiverValue: JSON,
    fnName: String!,
    fnArgs: JSON!,
  ): JSON! {
    ("null" :: JSON!)
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.dang"), []byte(old), 0o600))

	err := Check(context.Background(), dir, filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json"), dang.Version{})
	require.ErrorContains(t, err, "additional arguments not in interface must be optional")
}

func TestCheckRejectsInterfaceMismatch(t *testing.T) {
	dir := t.TempDir()
	invalid := `type Entrypoint implements ModuleEntrypoint {
  pub sources(workspace: Workspace!): Directory! { directory }
  pub types: [TypeDef!]! { [] }
  pub call(fnName: String!): JSON! { ("null" :: JSON!) }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.dang"), []byte(invalid), 0o600))

	err := Check(context.Background(), dir, filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json"), dang.Version{})
	require.ErrorContains(t, err, "does not satisfy ModuleEntrypoint")
}

func TestCheckRejectsArgumentList(t *testing.T) {
	dir := t.TempDir()
	invalid := `type Entrypoint implements ModuleEntrypoint {
  pub sources(workspace: Workspace!): Directory! { directory }
  pub types: [TypeDef!]! { [] }
  pub call(
    receiverType: String!,
    receiverValue: JSON,
    fnName: String!,
    fnArgs: [FunctionCallArgValue!]!,
  ): JSON! {
    ("null" :: JSON!)
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.dang"), []byte(invalid), 0o600))

	err := Check(context.Background(), dir, filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json"), dang.Version{})
	require.ErrorContains(t, err, "does not satisfy ModuleEntrypoint")
}
