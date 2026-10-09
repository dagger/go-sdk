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

func TestRunDefaultClientCallFollowsLayout(t *testing.T) {
	for _, tc := range []struct {
		flags      []string
		want       string
		importCore bool
	}{
		{[]string{"--global-client"}, "return dag.Container().", false},
		{[]string{"--importable-package"}, "return dagger.Connect().Container().", false},
		{[]string{"--importable-package", "--unified-clients=false", "--global-client=false"}, "return dagger.Connect().Container().", false},
		{[]string{"--importable-package", "--global-client"}, "return dag.Container().", false},
		{[]string{"--importable-package", "--unified-clients"}, "return core.NewContainer().", true},
		{[]string{"--importable-package", "--unified-clients", "--global-client"}, "return dag.Container().", false},
	} {
		out := t.TempDir()
		args := append(append([]string{}, tc.flags...), "hello-world", filepath.Join("..", "..", "templates", "default"), out)
		if err := run(args); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(out, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		starter := string(data)
		if !strings.Contains(starter, tc.want) || strings.Contains(starter, `"dagger.io/dagger/core"`) != tc.importCore {
			t.Errorf("flags %v: starter does not call %q with core imported=%v:\n%s", tc.flags, tc.want, tc.importCore, starter)
		}
	}
}

// Only Go runtime modules use the legacy starter, and they always keep dag.
func TestRunLegacyUsesGlobalClient(t *testing.T) {
	out := t.TempDir()
	if err := run([]string{"hello-world", filepath.Join("..", "..", "templates", "legacy"), out}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "dag.Container()") != 2 || strings.Contains(string(data), "Connect()") {
		t.Errorf("legacy starter does not use the global dag:\n%s", data)
	}
}

func TestRunStandaloneModuleInsideProject(t *testing.T) {
	project := t.TempDir()
	parentMod := "module example.com/project\n\ngo 1.25\n"
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte(parentMod), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(project, ".dagger", "hello")
	if err := run([]string{"--importable-package", "--standalone-go-module", "hello-world", filepath.Join("..", "..", "templates", "default"), out}); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(out, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(mod) != "module dagger/hello-world\n\ngo 1.26.1\n" {
		t.Fatalf("unexpected local module:\n%s", mod)
	}
	starter, err := os.ReadFile(filepath.Join(out, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(starter), `"dagger/hello-world/internal/dagger"`) || strings.Contains(string(starter), "dag.") {
		t.Fatalf("starter does not use its own explicit client:\n%s", starter)
	}
	parent, err := os.ReadFile(filepath.Join(project, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(parent) != parentMod {
		t.Fatal("creating the nested module changed the enclosing project")
	}
}

func TestStandaloneModuleRefusesConflictingOptions(t *testing.T) {
	for _, options := range [][]string{
		{"--standalone-go-module"},
		{"--importable-package", "--standalone-go-module", "--go-mod", "parent/go.mod"},
		{"--importable-package", "--standalone-go-module", "--module-subpath", "child"},
	} {
		if err := run(append(options, "hello", filepath.Join("..", "..", "templates", "default"), t.TempDir())); err == nil {
			t.Fatalf("conflicting standalone options accepted: %v", options)
		}
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
