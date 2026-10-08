package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/psanford/memfs"
)

func runPruneClients(args []string) error {
	flags := flag.NewFlagSet("prune-clients", flag.ContinueOnError)
	root := flags.String("root", ".", "directory containing generated client packages")
	keep := flags.String("keep", "", "comma-separated configured client names")
	removedPath := flags.String("removed-list", "", "file receiving removed paths relative to --root")
	prefix := flags.String("prefix", "", "prefix for reported removed paths")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	wanted := map[string]bool{}
	for _, name := range strings.Split(*keep, ",") {
		wanted[name] = true
	}
	entries, err := os.ReadDir(*root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ctx := context.Background()
	// Validate the entire batch before removing anything.
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() || wanted[entry.Name()] {
			continue
		}
		path := filepath.Join(*root, entry.Name())
		if _, err := os.Lstat(filepath.Join(path, generator.OwnershipFile)); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := generator.ValidateOwnedOverlay(ctx, memfs.New(), path); err != nil {
			return err
		}
		candidates = append(candidates, entry.Name())
	}
	var removed []string
	for _, name := range candidates {
		paths, err := generator.WriteOwnedOverlay(ctx, memfs.New(), filepath.Join(*root, name))
		if err != nil {
			return err
		}
		for _, path := range paths {
			removed = append(removed, filepath.ToSlash(filepath.Join(*prefix, name, path)))
		}
	}
	sort.Strings(removed)
	if *removedPath != "" {
		f, err := os.OpenFile(*removedPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		for _, path := range removed {
			if _, err := fmt.Fprintln(f, path); err != nil {
				f.Close()
				return err
			}
		}
		return f.Close()
	}
	return nil
}
