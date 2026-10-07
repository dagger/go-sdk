package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vito/dang/v2/pkg/dang"
	"github.com/vito/dang/v2/pkg/introspection"
)

// moduleEntrypointContract is the engine's ModuleEntrypoint interface, copied
// verbatim from moduleEntrypointInterface in dagger/dagger
// core/sdk/dang/v2/entrypoint.go (v1.0.0-beta.14).
const moduleEntrypointContract = `interface ModuleEntrypoint {
  types(workspace: Workspace!): [TypeDef!]!
  call(
    workspace: Workspace!
    receiverType: String!
    receiverValue: JSON
    fnName: String!
    fnArgs: JSON!
  ): JSON!
}
`

// Check type-checks a generated Dang entrypoint against the ModuleEntrypoint
// interface and the selected Dagger schema, the way the engine's Dang
// entrypoint loader installs that interface next to the entrypoint source.
func Check(ctx context.Context, entrypointDir, schemaPath string) error {
	schema, err := loadSchema(schemaPath)
	if err != nil {
		return err
	}

	testDir, err := os.MkdirTemp("", "dagger-entrypoint-contract-")
	if err != nil {
		return fmt.Errorf("create contract directory: %w", err)
	}
	defer os.RemoveAll(testDir)

	entries, err := os.ReadDir(entrypointDir)
	if err != nil {
		return fmt.Errorf("read entrypoint directory: %w", err)
	}
	sourceCount := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".dang" {
			continue
		}
		sourceCount++
		data, err := os.ReadFile(filepath.Join(entrypointDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read entrypoint source %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(testDir, entry.Name()), data, 0o600); err != nil {
			return fmt.Errorf("copy entrypoint source %s: %w", entry.Name(), err)
		}
	}
	if sourceCount == 0 {
		return fmt.Errorf("entrypoint directory has no Dang source")
	}
	if err := os.WriteFile(filepath.Join(testDir, "module-entrypoint-contract.dang"), []byte(moduleEntrypointContract), 0o600); err != nil {
		return fmt.Errorf("write module entrypoint contract: %w", err)
	}

	ctx = dang.ContextWithImportConfigs(ctx, dang.ImportConfig{
		Name:       "Dagger",
		Schema:     schema,
		AutoImport: true,
		Dagger:     true,
	})
	if _, err := dang.RunDir(ctx, testDir, false); err != nil {
		return fmt.Errorf("entrypoint does not satisfy ModuleEntrypoint: %w", err)
	}
	return nil
}

func loadSchema(path string) (*introspection.Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read introspection JSON: %w", err)
	}
	var response introspection.Response
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode introspection JSON: %w", err)
	}
	if response.Schema != nil && len(response.Schema.Types) > 0 {
		return response.Schema, nil
	}
	var schema introspection.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("decode raw introspection schema: %w", err)
	}
	if len(schema.Types) == 0 {
		return nil, fmt.Errorf("introspection JSON has no __schema")
	}
	return &schema, nil
}
