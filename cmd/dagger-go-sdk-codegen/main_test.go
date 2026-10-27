package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
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
