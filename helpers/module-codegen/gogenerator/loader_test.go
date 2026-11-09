package gogenerator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestStdlibImportErrors(t *testing.T) {
	exportData := packages.Error{Pos: "-", Msg: `cannot decode "context", export data version 5 is greater than maximum supported version 4`}
	cgoOnly := packages.Error{Pos: "-", Msg: "fatal error: missing-header.h: No such file or directory"}
	missing := packages.Error{Pos: "-", Msg: "no required module provides package example.com/missing"}
	pkg := &packages.Package{Imports: map[string]*packages.Package{
		"context": {PkgPath: "context", Errors: []packages.Error{exportData}},
		"fmt":     {PkgPath: "fmt"},
		// A dependency that fails only with cgo, which the build disables.
		"example.com/app/word": {
			PkgPath: "example.com/app/word",
			Module:  &packages.Module{Path: "example.com/app"},
			Errors:  []packages.Error{cgoOnly},
		},
		"example.com/missing": {PkgPath: "example.com/missing", Errors: []packages.Error{missing}},
	}}

	err := stdlibImportErrors(pkg)
	require.EqualError(t, err, "import context: -: "+exportData.Msg)

	delete(pkg.Imports, "context")
	require.NoError(t, stdlibImportErrors(pkg))
}

// A standard library package the loader cannot read is reported by name. A
// toolexec wrapper fails to compile context, as export data the loader
// cannot decode would.
func TestGenerateReportsUnreadableStdlibImport(t *testing.T) {
	root := t.TempDir()
	source := "package hello\n\nimport \"context\"\n\ntype Hello struct{}\n\nfunc (h *Hello) Echo(ctx context.Context, value string) string { return value }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644))
	wrapper := filepath.Join(t.TempDir(), "toolexec")
	script := "#!/bin/sh\ncase \" $* \" in *\" -p context \"*) echo cannot read context >&2; exit 1;; esac\nexec \"$@\"\n"
	require.NoError(t, os.WriteFile(wrapper, []byte(script), 0755))
	t.Setenv("GOFLAGS", "-toolexec="+wrapper)
	// The build cache does not key on toolexec, and this test binary's own
	// build already cached context.
	t.Setenv("GOCACHE", t.TempDir())
	cfg := GenerateConfig{ModuleRoot: root, ModuleName: "hello", SchemaPath: moduleContextSchema(t), SchemaVersion: "v1.0.0-beta.16", DaggerVersion: "v1.0.0-beta.14", GoImage: "golang:1.26-alpine"}
	err := Generate(t.Context(), cfg)
	require.ErrorContains(t, err, "reload module package: import context:")
}
