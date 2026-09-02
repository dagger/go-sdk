package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"module-codegen/gogenerator"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "module-codegen:", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg gogenerator.GenerateConfig
	flag.StringVar(&cfg.ModuleRoot, "module-root", "", "path to the Go module implementation")
	flag.StringVar(&cfg.ModuleName, "module-name", "", "Dagger module name")
	flag.StringVar(&cfg.SchemaPath, "introspection-json-path", "", "path to the module-facing introspection JSON")
	flag.StringVar(&cfg.SchemaVersion, "schema-version", "", "engine schema version")
	flag.StringVar(&cfg.DaggerVersion, "dagger-version", "", "dagger.io/dagger version")
	flag.StringVar(&cfg.GoImage, "go-image", "golang:1.26-alpine", "Go image used by the generated entrypoint")
	flag.BoolVar(&cfg.CoreOnly, "core-only", false, "remove module-contributed types from the input schema")
	flag.BoolVar(&cfg.RemoveLegacyManifest, "remove-legacy-manifest", false, "remove dagger.json after writing the v2 manifest")
	flag.Parse()

	if cfg.ModuleRoot == "" {
		return fmt.Errorf("--module-root is required")
	}
	if cfg.ModuleName == "" {
		return fmt.Errorf("--module-name is required")
	}
	if cfg.SchemaPath == "" {
		return fmt.Errorf("--introspection-json-path is required")
	}
	return gogenerator.Generate(context.Background(), cfg)
}
