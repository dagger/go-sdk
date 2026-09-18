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
