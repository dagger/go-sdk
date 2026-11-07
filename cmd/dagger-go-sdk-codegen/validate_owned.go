package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
)

// Validate only prospective output paths, which source filtering may omit.
func runValidateOwned(args []string) error {
	flags := flag.NewFlagSet("validate-owned", flag.ContinueOnError)
	root := flags.String("root", "", "existing generated directory")
	generated := flags.String("generated", "", "prospective generated files, without ownership metadata")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *root == "" || *generated == "" || flags.NArg() != 0 {
		return fmt.Errorf("validate-owned needs --root and --generated")
	}
	return generator.ValidateOwnedOverlay(context.Background(), os.DirFS(*generated), *root)
}
