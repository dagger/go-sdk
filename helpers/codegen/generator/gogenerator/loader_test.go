package gogenerator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeModule(t *testing.T, mainGo string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/probe\n\ngo 1.22\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o600))
	return dir
}

// An import used only inside a function body reports "imported and not used"
// once bodies are stripped; that is a TypeError and must not fail the load.
func TestLoadPackageToleratesBodyOnlyImport(t *testing.T) {
	dir := writeModule(t, `package main

import "fmt"

type Probe struct{}

func (p *Probe) Hello(name string) string { return fmt.Sprintf("hello %s", name) }

func main() {}
`)

	pkg, fset, err := loadPackage(context.Background(), dir, false)
	require.NoError(t, err)
	require.NotNil(t, fset)
	require.Equal(t, "main", pkg.Name)
	require.NotNil(t, pkg.Types.Scope().Lookup("Probe"))
}

// A module that cannot be resolved surfaces as a ListError and must fail,
// rather than emitting bindings with the affected functions silently missing.
func TestLoadPackageRejectsUnresolvableImport(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	dir := writeModule(t, `package main

import "example.invalid/private/lib"

type Probe struct{}

func (p *Probe) Thing() *lib.Thing { return nil }

func main() {}
`)

	_, _, err := loadPackage(context.Background(), dir, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "example.invalid/private/lib")
}
