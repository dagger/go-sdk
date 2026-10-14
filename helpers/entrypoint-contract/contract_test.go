package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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

	err := Check(context.Background(), dir, filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json"))
	require.NoError(t, err)
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

	err := Check(context.Background(), dir, filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json"))
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

	err := Check(context.Background(), dir, filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json"))
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

	err := Check(context.Background(), dir, filepath.Join("..", "..", "cmd", "dagger-go-sdk-codegen", "introspection", "testdata", "schema.json"))
	require.ErrorContains(t, err, "does not satisfy ModuleEntrypoint")
}
