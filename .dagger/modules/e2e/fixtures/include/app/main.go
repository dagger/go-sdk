//go:mod:include ../lib/go.mod ../lib/greet !../lib/greet/testdata

// Package include_app builds with a package of a sibling Go module.
package include_app

import (
	"os"

	"example.com/lib/greet"
)

type IncludeApp struct{}

// Greeting returns the text that lib/greet embeds when the module is built.
func (*IncludeApp) Greeting() string { return greet.Greeting() }

// Runtime reads the same file when the function runs.
func (*IncludeApp) Runtime() (string, error) {
	data, err := os.ReadFile("../lib/greet/greeting.txt")
	return string(data), err
}

// Excluded reports whether the excluded testdata directory reached the module.
func (*IncludeApp) Excluded() bool {
	_, err := os.Stat("../lib/greet/testdata")
	return err == nil
}
