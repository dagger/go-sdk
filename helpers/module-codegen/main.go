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
	"strings"

	"module-codegen/discovery"
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
	if len(os.Args) > 1 && os.Args[1] == "replaces" {
		return runReplaces(os.Args[2:])
	}
	var cfg gogenerator.GenerateConfig
	flag.StringVar(&cfg.BuildRoot, "build-root", "", "workspace snapshot containing compilation inputs")
	flag.StringVar(&cfg.ModuleRoot, "module-root", "", "path to the Go module implementation")
	flag.StringVar(&cfg.ModuleName, "module-name", "", "Dagger module name")
	flag.StringVar(&cfg.SchemaPath, "introspection-json-path", "", "path to the module-facing introspection JSON")
	flag.StringVar(&cfg.SchemaVersion, "schema-version", "", "engine schema version")
	flag.StringVar(&cfg.DaggerVersion, "dagger-version", "", "dagger.io/dagger version")
	flag.StringVar(&cfg.TargetVersion, "target-version", "", "Dagger release the generated code targets, which the entrypoint declares to the module's process")
	flag.StringVar(&cfg.GoImage, "go-image", "golang:1.26-alpine", "Go image used by the generated entrypoint")
	flag.StringVar(&cfg.RemovedPath, "removed-list", "", "file that receives the paths of removed generated files")
	flag.BoolVar(&cfg.UnifiedClient, "unified", false, "reuse core bindings and shared sessions")
	flag.BoolVar(&cfg.GlobalClient, "global-client", false, "generate the compatibility unqualified dag API")
	flag.Parse()
	cfg.Notes = os.Stdout

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

// runIncludes writes what the module's build reads outside its module
// directory to a directory, one file per part, so the caller can read those
// paths from its workspace before generation. The include file holds one JSON
// value per line.
func runIncludes(args []string) error {
	flags := flag.NewFlagSet("includes", flag.ContinueOnError)
	root := flags.String("module-root", "", "path to the Go module implementation")
	workspacePath := flags.String("workspace-path", "", "module directory relative to the workspace root")
	output := flags.String("output", "", "directory that receives the include list")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *root == "" || *workspacePath == "" || *output == "" {
		return fmt.Errorf("includes needs --module-root, --workspace-path and --output")
	}
	includes, err := discovery.ReadIncludes(*root, *workspacePath)
	if err != nil {
		return err
	}
	var lines bytes.Buffer
	for _, include := range includes.Include {
		line, err := json.Marshal(includeEntry{Position: include.Position, Path: include.Path, Pattern: include.Pattern})
		if err != nil {
			return err
		}
		lines.Write(append(line, '\n'))
	}
	files := map[string][]byte{
		"anchor":  []byte(path.Join(*workspacePath, includes.Anchor)),
		"module":  []byte(includes.ModulePath),
		"include": lines.Bytes(),
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

// runReplaces prints the local replace targets of go.mod outside the module
// directory and inside the workspace, relative to the workspace root, one per
// line, so the caller can read their sources before it scans the module.
func runReplaces(args []string) error {
	flags := flag.NewFlagSet("replaces", flag.ContinueOnError)
	root := flags.String("module-root", "", "path to the Go module implementation")
	workspacePath := flags.String("workspace-path", "", "module directory relative to the workspace root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *root == "" || *workspacePath == "" {
		return fmt.Errorf("replaces needs --module-root and --workspace-path")
	}
	targets, err := discovery.LocalReplaceTargets(*root)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if p := path.Join(*workspacePath, target); p != ".." && !strings.HasPrefix(p, "../") {
			fmt.Println(p)
		}
	}
	return nil
}
