package gogenerator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRefusesPackageMain(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\n\ntype HelloWorld struct{}\n")

	err := Generate(t.Context(), GenerateConfig{ModuleRoot: root, ModuleName: "hello-world"})
	if err == nil || !strings.Contains(err.Error(), "change package main to package hello_world") {
		t.Fatalf("Generate() error = %v, want the package main migration message", err)
	}
}

func TestSourcePackageNameRejectsMixedPackages(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.go"), "package hello\n")
	writeTestFile(t, filepath.Join(root, "b.go"), "package other\n")

	if _, err := sourcePackageName(root); err == nil {
		t.Fatal("sourcePackageName() accepted two packages")
	}
}

func TestEnsureGoModuleUsesEnclosingModule(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.25\n")
	moduleRoot := filepath.Join(root, "mods", "hello")
	if err := os.MkdirAll(moduleRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	packageImport, goModPath, err := ensureGoModule(moduleRoot, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if packageImport != "example.com/app/mods/hello" {
		t.Errorf("package import = %q, want example.com/app/mods/hello", packageImport)
	}
	if goModPath != filepath.Join(root, "go.mod") {
		t.Errorf("go.mod path = %q, want the enclosing go.mod", goModPath)
	}
	if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err == nil {
		t.Error("a nested go.mod was created")
	}
}

func TestPinDaggerKeepsReplaceAndNewerVersion(t *testing.T) {
	for name, goMod := range map[string]string{
		"replace":        "module example.com/app\n\ngo 1.25\n\nreplace dagger.io/dagger => ../sdk\n",
		"newer required": "module example.com/app\n\ngo 1.25\n\nrequire dagger.io/dagger v1.0.0-beta.20\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "go.mod")
			writeTestFile(t, path, goMod)

			if err := pinDagger(path, "v1.0.0-beta.14"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != goMod {
				t.Errorf("go.mod changed:\n%s", data)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
