package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codegen/generator"
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

func TestUpdateModuleGoMod(t *testing.T) {
	t.Run("adds the requirement", func(t *testing.T) {
		root := t.TempDir()
		goModPath := filepath.Join(root, "go.mod")
		if err := os.WriteFile(goModPath, []byte("module example.com/app\n\ngo 1.25\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := updateModuleGoMod(root, "v1.0.0-beta.11"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(goModPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "dagger.io/dagger v1.0.0-beta.11") {
			t.Fatalf("updated go.mod does not contain Dagger requirement:\n%s", data)
		}
	})

	t.Run("does not lower the requirement", func(t *testing.T) {
		root := t.TempDir()
		goModPath := filepath.Join(root, "go.mod")
		goMod := "module example.com/app\n\ngo 1.25\n\nrequire dagger.io/dagger v1.1.0\n"
		if err := os.WriteFile(goModPath, []byte(goMod), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := updateModuleGoMod(root, "v1.0.0"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(goModPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != goMod {
			t.Fatalf("go.mod changed:\n%s", data)
		}
	})

	t.Run("preserves a custom replace", func(t *testing.T) {
		root := t.TempDir()
		goModPath := filepath.Join(root, "go.mod")
		goMod := "module example.com/app\n\ngo 1.25\n\nrequire dagger.io/dagger v0.18.0\n\nreplace dagger.io/dagger => ../dagger\n"
		if err := os.WriteFile(goModPath, []byte(goMod), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := updateModuleGoMod(root, "v1.0.0"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(goModPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != goMod {
			t.Fatalf("go.mod changed:\n%s", data)
		}
	})
}
