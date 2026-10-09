package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"module-codegen/gogenerator"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "module-codegen:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "includes" {
		return runIncludes(os.Args[2:])
	}
	var cfg gogenerator.GenerateConfig
	flag.StringVar(&cfg.ModuleRoot, "module-root", "", "path to the Go module implementation")
	flag.StringVar(&cfg.ModuleName, "module-name", "", "Dagger module name")
	flag.StringVar(&cfg.SchemaPath, "introspection-json-path", "", "path to the module-facing introspection JSON")
	flag.StringVar(&cfg.SchemaVersion, "schema-version", "", "engine schema version")
	flag.StringVar(&cfg.DaggerVersion, "dagger-version", "", "dagger.io/dagger version")
	flag.StringVar(&cfg.GoImage, "go-image", "golang:1.26-alpine", "Go image used by the generated entrypoint")
	flag.StringVar(&cfg.RemovedPath, "removed-list", "", "file that receives the paths of removed generated files")
	flag.BoolVar(&cfg.UnifiedClient, "unified", false, "reuse core bindings and shared sessions")
	flag.BoolVar(&cfg.GlobalClient, "global-client", false, "generate the compatibility unqualified dag API")
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

type includeEntry struct {
	Position string `json:"position"`
	Path     string `json:"path"`
	Pattern  string `json:"pattern"`
}

// runIncludes writes the module's //go:mod:include list to a directory, one
// file per part, so the caller can read the listed paths from its workspace
// before generation. List files hold one JSON value per line.
func runIncludes(args []string) error {
	flags := flag.NewFlagSet("includes", flag.ContinueOnError)
	root := flags.String("module-root", "", "path to the Go module implementation")
	name := flags.String("module-name", "", "Dagger module name")
	workspacePath := flags.String("workspace-path", "", "module directory relative to the workspace root")
	output := flags.String("output", "", "directory that receives the include list")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *root == "" || *name == "" || *workspacePath == "" || *output == "" {
		return fmt.Errorf("includes needs --module-root, --module-name, --workspace-path and --output")
	}
	includes, err := gogenerator.ReadIncludes(*root, *name, *workspacePath)
	if err != nil {
		return err
	}
	entries := make([]any, len(includes.Include))
	for i, include := range includes.Include {
		entries[i] = includeEntry(include)
	}
	files := map[string][]byte{
		"anchor": []byte(path.Join(*workspacePath, includes.Anchor)),
		"module": []byte(includes.ModulePath),
	}
	for name, values := range map[string][]any{
		"include":        entries,
		"exclude":        anySlice(includes.Exclude),
		"module-exclude": anySlice(includes.ModuleExclude),
	} {
		var lines bytes.Buffer
		for _, value := range values {
			line, err := json.Marshal(value)
			if err != nil {
				return err
			}
			lines.Write(append(line, '\n'))
		}
		files[name] = lines.Bytes()
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		return err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(*output, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func anySlice(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}
