package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func workspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, data := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestTransitiveInputsAndEmbeds(t *testing.T) {
	root := workspace(t, map[string]string{
		"app/go.mod":               "module example/app\n\ngo 1.26\nrequire example/lib v0.0.0\nreplace example/lib => ../lib\n",
		"app/main.go":              "package app\nimport _ \"example/lib/greet\"\n",
		"lib/go.mod":               "module example/lib\n\ngo 1.26\n",
		"lib/greet/greet.go":       "package greet\nimport (_ \"embed\"; _ \"example/lib/shared\")\n//go:embed text\nvar text string\n",
		"lib/greet/text/hello.txt": "hello",
		"lib/greet/text/.hidden":   "hidden",
		"lib/greet/text/_hidden":   "hidden",
		"lib/shared/shared.go":     "package shared\n",
		"lib/shared/source.s":      "",
		"lib/unused/unused.go":     "package unused\n",
		"lib/shared/README":        "unrelated",
	})
	inputs, err := Discover(root, filepath.Join(root, "app"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"app/go.mod", "app/main.go", "lib/go.mod", "lib/greet/greet.go", "lib/greet/text/.hidden", "lib/greet/text/_hidden", "lib/greet/text/hello.txt", "lib/shared/shared.go", "lib/shared/source.s"}
	if !inputs.External || !reflect.DeepEqual(want, inputs.Files) {
		t.Fatalf("inputs = %#v", inputs)
	}
	// Discovery is repeated: changing a dependency's imports selects new files
	// without regenerating the module or editing an authored resource list.
	if err := os.WriteFile(filepath.Join(root, "lib/shared/shared.go"), []byte("package shared\nimport _ \"example/lib/unused\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	inputs, err = Discover(root, filepath.Join(root, "app"), nil)
	if err != nil || !slices.Contains(inputs.Files, "lib/unused/unused.go") {
		t.Fatalf("new import: %#v, %v", inputs, err)
	}
}

func TestMetadataSnapshotUsesInventory(t *testing.T) {
	root := workspace(t, map[string]string{
		"app/go.mod":  "module example/app\n\ngo 1.26\n",
		"app/main.go": "package app\nimport _ \"embed\"\n//go:embed all:text\nvar text string\n",
	})
	inputs, err := Discover(root, filepath.Join(root, "app"), []string{"app/main.go", "app/text/hello.txt", "app/text/.hidden", "app/text/"})
	if err != nil {
		t.Fatal(err)
	}
	if inputs.External || !slices.Contains(inputs.Files, "app/text/.hidden") {
		t.Fatalf("inputs = %#v", inputs)
	}
}

func TestMetadataPatternsSelectEmbedsAndNativeFilesWithoutInventory(t *testing.T) {
	root := workspace(t, map[string]string{
		"app/go.mod":             "module example/app\n\ngo 1.26\nrequire example/lib v0.0.0\nreplace example/lib => ../lib\n",
		"app/main.go":            "package app\nimport _ \"example/lib/greet\"\n",
		"lib/go.mod":             "module example/lib\n\ngo 1.26\n",
		"lib/greet/greet.go":     "package greet\nimport (_ \"embed\"; _ \"example/lib/shared\")\n//go:embed all:text\nvar text string\n",
		"lib/shared/shared.go":   "package shared\n",
		"unrelated/unrelated.go": "package unrelated\n",
	})
	inputs, err := DiscoverPatterns(root, filepath.Join(root, "app"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"app", "lib/go.mod", "lib/greet/*.go", "lib/greet/*.h", "lib/greet/*.s", "lib/greet/*.syso", "lib/greet/text", "lib/shared/*.go", "lib/shared/*.h", "lib/shared/*.s", "lib/shared/*.syso"}
	if !inputs.External || !reflect.DeepEqual(inputs.Files, want) {
		t.Fatalf("patterns = %#v", inputs)
	}
	// Asset names and contents need not be present in the metadata snapshot.
	// Changing them does not require changing the selection patterns.
	if err := os.MkdirAll(filepath.Join(root, "lib/greet/text"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib/greet/text/new.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	next, err := DiscoverPatterns(root, filepath.Join(root, "app"))
	if err != nil || !reflect.DeepEqual(inputs, next) {
		t.Fatalf("patterns changed with asset contents: %#v, %v", next, err)
	}
	// Import and directive changes still produce a fresh selection.
	if err := os.WriteFile(filepath.Join(root, "lib/greet/greet.go"), []byte("package greet\nimport _ \"embed\"\n//go:embed other.txt\nvar text string\n"), 0644); err != nil {
		t.Fatal(err)
	}
	next, err = DiscoverPatterns(root, filepath.Join(root, "app"))
	if err != nil || !slices.Contains(next.Files, "lib/greet/other.txt") || slices.Contains(next.Files, "lib/shared/*.go") || slices.Contains(next.Files, "lib/greet/text") {
		t.Fatalf("patterns after changed imports/embed: %#v, %v", next, err)
	}
}

func TestMetadataPatternsRefuseReplacementOutsideWorkspace(t *testing.T) {
	root := workspace(t, map[string]string{"app/go.mod": "module example/app\n\ngo 1.26\nreplace example/lib => ../../outside\n"})
	if _, err := DiscoverPatterns(root, filepath.Join(root, "app")); err == nil || !strings.Contains(err.Error(), "leaves the workspace") {
		t.Fatalf("error = %v", err)
	}
}

func TestImportsUnderAllTagsAndTests(t *testing.T) {
	root := workspace(t, map[string]string{
		"app/go.mod":               "module example/app\n\ngo 1.26\nrequire example/lib v0.0.0\nreplace example/lib => ../lib\n",
		"app/main.go":              "package app\n",
		"app/test_test.go":         "package app\nimport _ \"example/lib/testing\"\n",
		"app/other_windows.go":     "//go:build windows\n\npackage app\nimport _ \"example/lib/windows\"\n",
		"lib/go.mod":               "module example/lib\n\ngo 1.26\n",
		"lib/testing/test.go":      "package testing\n",
		"lib/windows/windows.go":   "package windows\n",
		"lib/testing/test_test.go": "package testing\nimport _ \"example/lib/unused\"\n",
	})
	inputs, err := Discover(root, filepath.Join(root, "app"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(inputs.Files, "lib/windows/windows.go") || !slices.Contains(inputs.Files, "lib/testing/test.go") {
		t.Fatalf("inputs = %#v", inputs)
	}
	if slices.Contains(inputs.Files, "lib/testing/test_test.go") {
		t.Fatal("dependency tests selected")
	}
}

func TestReplaceCannotLeaveWorkspace(t *testing.T) {
	root := workspace(t, map[string]string{
		"app/go.mod": "module example/app\n\ngo 1.26\nreplace example/lib => ../../outside\n",
	})
	_, err := Discover(root, filepath.Join(root, "app"), nil)
	if err == nil || !strings.Contains(err.Error(), "leaves the workspace") {
		t.Fatalf("error = %v", err)
	}
}

func TestInputsBeforeGoModIsCreated(t *testing.T) {
	root := workspace(t, map[string]string{
		"app/main.go":  "package app\nimport \"fmt\"\nfunc Hello() string { return fmt.Sprint(\"hello\") }\n",
		"app/own.txt":  "module asset",
		"other/README": "unrelated",
	})
	inputs, err := Discover(root, filepath.Join(root, "app"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"app/main.go", "app/own.txt"}
	if inputs.External || !reflect.DeepEqual(want, inputs.Files) {
		t.Fatalf("inputs = %#v", inputs)
	}
}
