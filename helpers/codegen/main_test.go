package main

import (
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

func TestModuleFlagsRequired(t *testing.T) {
	for _, tt := range []struct {
		name    string
		flags   moduleFlags
		wantErr string
	}{
		{name: "complete", flags: moduleFlags{name: "app", rootPath: ".", sourcePath: ".", libVersion: "v1"}},
		{name: "missing name", flags: moduleFlags{rootPath: ".", sourcePath: ".", libVersion: "v1"}, wantErr: "--module-name is required"},
		{name: "missing root", flags: moduleFlags{name: "app", sourcePath: ".", libVersion: "v1"}, wantErr: "--module-root-path is required"},
		{name: "missing source", flags: moduleFlags{name: "app", rootPath: ".", libVersion: "v1"}, wantErr: "--module-source-path is required"},
		{name: "missing lib version", flags: moduleFlags{name: "app", rootPath: ".", sourcePath: "."}, wantErr: "--lib-version is required"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.flags.config()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("want %q, got %v", tt.wantErr, err)
			}
		})
	}
}
