package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/stretchr/testify/require"
)

func TestValidateBoundModuleKind(t *testing.T) {
	tests := []struct {
		name    string
		mod     generator.BoundModule
		wantErr bool
	}{
		{name: "git", mod: generator.BoundModule{Kind: "GIT_SOURCE", Ref: "github.com/foo/bar@main", Pin: "abc"}},
		{name: "local", mod: generator.BoundModule{Kind: "LOCAL_SOURCE", Path: "/mods/bar"}},
		{name: "dir (local module resolves as dir)", mod: generator.BoundModule{Kind: "DIR_SOURCE", Path: "/mods/bar"}},
		{name: "unknown rejected", mod: generator.BoundModule{Kind: "WAT"}, wantErr: true},
		{name: "empty rejected", mod: generator.BoundModule{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBoundModuleKind(tt.mod)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCheckLibraryReplacement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replace string
		wantErr bool
	}{
		{"target only", " v1.0.0-beta.20", true},
		{"selected version", " v1.0.0-beta.21", false},
		{"all versions", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26.1\n\nrequire dagger.io/dagger v1.0.0-beta.21\n\nreplace dagger.io/dagger"+tc.replace+" => dagger.io/dagger v1.0.0-beta.21\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package app\n\nimport _ \"dagger.io/dagger\"\n"), 0o600))
			tidy := exec.Command("go", "mod", "tidy")
			tidy.Dir = root
			out, err := tidy.CombinedOutput()
			require.NoError(t, err, string(out))
			err = runCheckLibrary([]string{"--module-root", root, "--target", "v1.0.0-beta.20", "--subject", `scope "ci"`})
			if tc.wantErr {
				require.ErrorContains(t, err, `scope "ci" targets Dagger v1.0.0-beta.20, and go.mod selects dagger.io/dagger v1.0.0-beta.21`)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPackageImportPath(t *testing.T) {
	root := t.TempDir()
	clientDir := filepath.Join(root, "internal", "dagger", "clients", "hello")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := packageImportPath(root, clientDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "example.com/app/internal/dagger/clients/hello"; got != want {
		t.Fatalf("package import = %q, want %q", got, want)
	}
}
