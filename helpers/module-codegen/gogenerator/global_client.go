package gogenerator

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
)

// resolveGlobalClient runs before generated files are pruned. After migration
// the ownership metadata remembers the choice, even when the old Go runtime
// manifest or generated source no longer exists.
func resolveGlobalClient(root, policy string, ownership generator.Ownership) (bool, error) {
	switch policy {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "", "auto":
	default:
		return false, fmt.Errorf("--global-client must be auto, true, or false; got %q", policy)
	}
	if ownership.Compatibility != nil && ownership.Compatibility.GlobalClient != nil {
		return *ownership.Compatibility.GlobalClient, nil
	}

	data, err := readOptionalFile(filepath.Join(root, "dagger.gen.go"))
	if err != nil {
		return false, err
	}
	if generatedMarker.Match(data) {
		file, err := parser.ParseFile(token.NewFileSet(), "dagger.gen.go", data, 0)
		if err != nil {
			return false, fmt.Errorf("read legacy generated bindings: %w", err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if name.Name == "dag" {
						return true, nil
					}
				}
			}
		}
	}

	data, err = readOptionalFile(filepath.Join(root, "dagger.json"))
	if err != nil {
		return false, err
	}
	if len(data) > 0 {
		var manifest struct {
			SDK json.RawMessage `json:"sdk"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			return false, fmt.Errorf("read legacy Go manifest: %w", err)
		}
		var source string
		if json.Unmarshal(manifest.SDK, &source) == nil && source == "go" {
			return true, nil
		}
		var sdk struct {
			Source string `json:"source"`
		}
		if json.Unmarshal(manifest.SDK, &sdk) == nil && sdk.Source == "go" {
			return true, nil
		}
	}

	// Only an explicit built-in Go runtime counts. Decode TOML rather than
	// depending on formatting: inline, dotted, and quoted tables are all valid.
	data, err = readOptionalFile(filepath.Join(root, "dagger-module.toml"))
	if err != nil {
		return false, err
	}
	var manifest struct {
		Runtime struct {
			Source string `toml:"source"`
		} `toml:"runtime"`
	}
	if _, err := toml.Decode(string(data), &manifest); err != nil {
		return false, fmt.Errorf("read legacy Go manifest: %w", err)
	}
	return manifest.Runtime.Source == "go", nil
}

func readOptionalFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read global-client migration state: %w", err)
	}
	return data, nil
}
