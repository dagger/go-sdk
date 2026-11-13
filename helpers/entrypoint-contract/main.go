package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/vito/dang/v2/pkg/dang"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "entrypoint-contract:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("entrypoint-contract", flag.ContinueOnError)
	entrypointDir := flags.String("entrypoint-dir", "", "directory that contains generated Dang entrypoint source")
	schemaPath := flags.String("introspection-json-path", "", "Dagger introspection JSON used to type-check the entrypoint")
	dangVersion := flags.String("dang-version", "", "the Dang version the entrypoint's dang.toml declares, which the engine evaluates it at; empty for Dang's current version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *entrypointDir == "" {
		return fmt.Errorf("--entrypoint-dir is required")
	}
	if *schemaPath == "" {
		return fmt.Errorf("--introspection-json-path is required")
	}
	var version dang.Version
	if *dangVersion != "" {
		var err error
		if version, err = dang.ParseVersion(*dangVersion); err != nil {
			return fmt.Errorf("--dang-version: %w", err)
		}
	}
	return Check(ctx, *entrypointDir, *schemaPath, version)
}
