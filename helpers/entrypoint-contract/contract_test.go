package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const validEntrypoint = `type Entrypoint implements ModuleEntrypoint {
  pub types(workspace: Workspace!): [TypeDef!]! { [] }
  pub call(
    workspace: Workspace!,
    receiverType: String!,
    receiverValue: JSON,
    fnName: String!,
    fnArgs: [FunctionCallArgValue!]!,
  ): JSON! {
    JSON.decode("null")
  }
}
`

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.dang"), []byte(validEntrypoint), 0o600))

	err := Check(context.Background(), dir, filepath.Join("..", "codegen", "introspection", "testdata", "schema.json"))
	require.NoError(t, err)
}

func TestCheckRejectsInterfaceMismatch(t *testing.T) {
	dir := t.TempDir()
	invalid := `type Entrypoint implements ModuleEntrypoint {
  pub types(workspace: Workspace!): [TypeDef!]! { [] }
  pub call(workspace: Workspace!): JSON! { JSON.decode("null") }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.dang"), []byte(invalid), 0o600))

	err := Check(context.Background(), dir, filepath.Join("..", "codegen", "introspection", "testdata", "schema.json"))
	require.ErrorContains(t, err, "does not satisfy ModuleEntrypoint")
}
