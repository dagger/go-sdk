package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequiredGoVersion(t *testing.T) {
	tests := []struct {
		name    string
		goMod   string
		minimum string
		want    string
		wantErr bool
	}{
		{name: "go directive", goMod: "module example.com/app\n\ngo 1.26.1\n", want: "1.26"},
		{name: "newer toolchain", goMod: "module example.com/app\n\ngo 1.25\n\ntoolchain go1.26.2\n", want: "1.26"},
		{name: "older toolchain", goMod: "module example.com/app\n\ngo 1.26\n\ntoolchain go1.25.4\n", want: "1.26"},
		{name: "toolchain default", goMod: "module example.com/app\n\ngo 1.25\n\ntoolchain default\n", want: "1.25"},
		{name: "newer minimum", goMod: "module example.com/app\n\ngo 1.25\n", minimum: "1.26", want: "1.26"},
		{name: "older minimum", goMod: "module example.com/app\n\ngo 1.26\n", minimum: "1.25", want: "1.26"},
		{name: "newer consumer", goMod: "module example.com/app\n\ngo 1.27\n", minimum: "1.26", want: "1.27"},
		{name: "invalid minimum", goMod: "module example.com/app\n\ngo 1.25\n", minimum: "next", wantErr: true},
		{name: "missing go directive", goMod: "module example.com/app\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "go.mod")
			if err := os.WriteFile(path, []byte(tt.goMod), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := requiredGoVersion(path, tt.minimum)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("requiredGoVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
