package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModulePackage(t *testing.T) {
	tests := map[string]string{
		"hello-world": "hello_world",
		"123-build":   "module_123_build",
		"type":        "type_module",
		"":            "module",
	}
	for input, want := range tests {
		if got := modulePackage(input); got != want {
			t.Errorf("modulePackage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRunPackageName(t *testing.T) {
	for _, template := range []string{"default", "empty", "legacy"} {
		t.Run(template, func(t *testing.T) {
			templateDir := filepath.Join("..", "..", "templates", template)

			runtimeOut := t.TempDir()
			if err := run([]string{"hello-world", templateDir, runtimeOut}); err != nil {
				t.Fatal(err)
			}
			if got := packageClause(t, filepath.Join(runtimeOut, "main.go")); got != "package main" {
				t.Errorf("runtime template has %q, want package main", got)
			}

			importableOut := t.TempDir()
			if err := run([]string{"--importable-package", "hello-world", templateDir, importableOut}); err != nil {
				t.Fatal(err)
			}
			if got := packageClause(t, filepath.Join(importableOut, "main.go")); got != "package hello_world" {
				t.Errorf("importable template has %q, want package hello_world", got)
			}
		})
	}
}

func TestRunImportFromEnclosingGoModule(t *testing.T) {
	templateDir := filepath.Join("..", "..", "templates", "default")
	goMod := filepath.Join(t.TempDir(), "go.mod")
	if err := os.WriteFile(goMod, []byte("module example.com/app // the app\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for subpath, want := range map[string]string{
		".":          `"example.com/app/internal/dagger"`,
		"mods/hello": `"example.com/app/mods/hello/internal/dagger"`,
	} {
		out := t.TempDir()
		if err := run([]string{"--importable-package", "--go-mod", goMod, "--module-subpath", subpath, "hello", templateDir, out}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(out, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("subpath %s: starter does not import %s:\n%s", subpath, want, data)
		}
	}

	if err := run([]string{"--importable-package", "--go-mod", goMod, "--module-subpath", "../x", "hello", templateDir, t.TempDir()}); err == nil {
		t.Error("a module outside the Go module was accepted")
	}
}

func TestRunDefaultImport(t *testing.T) {
	out := t.TempDir()
	if err := run([]string{"--importable-package", "hello-world", filepath.Join("..", "..", "templates", "default"), out}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dagger/hello-world/internal/dagger"`) {
		t.Errorf("starter does not import the module's own go.mod path:\n%s", data)
	}
}

func packageClause(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "package ") {
			return line
		}
	}
	t.Fatalf("%s has no package clause", path)
	return ""
}
