package main

import "testing"

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
