package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vito/dang/v2/pkg/dang"
)

// Read the declaration as well as the selection functions from the SDK: a
// non-null default would erase the distinction between unset and explicit true.
func sdkSetting(t *testing.T, name string) string {
	t.Helper()
	decl := regexp.MustCompile(`(?m)^  ` + name + `:.*$`).FindString(sdkSource(t))
	require.NotEmpty(t, decl)
	return decl
}

func runSdkSelection(t *testing.T, program string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sdk.dang"), []byte(program), 0o600))
	_, err := dang.RunDir(context.Background(), dir, false)
	require.NoError(t, err)
}

func TestSdkEntrypointSelection(t *testing.T) {
	for _, tc := range []struct {
		name        string
		setting     string
		hadManifest bool
		generated   bool
		remote      string
		want        bool
	}{
		{name: "new module", want: true},
		{name: "new runtime module", setting: "false"},
		{name: "existing runtime or custom local runner", hadManifest: true},
		{name: "explicit runtime", setting: "false", hadManifest: true},
		{name: "explicit migration", setting: "true", hadManifest: true, want: true},
		{name: "generated entrypoint", hadManifest: true, generated: true, want: true},
		{name: "explicit false selects runtime for refusal", setting: "false", hadManifest: true, generated: true},
		{name: "remote entrypoint", hadManifest: true, remote: "github.com/example/sdk/entrypoint@v1", want: true},
		{name: "remote entrypoint with explicit false", setting: "false", hadManifest: true, remote: "github.com/example/sdk/entrypoint@v1", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting := ""
			if tc.setting != "" {
				setting = "dangEntrypoint: " + tc.setting
			}
			runSdkSelection(t, fmt.Sprintf(`
type Workspace {
  generated: Boolean!
  remote: String!
}
type Sdk {
%s
  let hasGeneratedEntrypoint(ws: Workspace!): Boolean! { ws.generated }
  let remoteEntrypoint(ws: Workspace!): String! { ws.remote }
%s
  pub selected(ws: Workspace!, hadManifest: Boolean!): Boolean! {
    usesEntrypoint(ws, hadManifest)
  }
}
let selected = Sdk(%s).selected(ws: Workspace(generated: %t, remote: %q), hadManifest: %t)
assert(%q) { selected == %t }
`, sdkSetting(t, "dangEntrypoint"), sdkMember(t, sdkSource(t), "usesEntrypoint("), setting, tc.generated, tc.remote, tc.hadManifest, tc.name, tc.want))
		})
	}
}

func TestSdkUnifiedSelection(t *testing.T) {
	for _, tc := range []struct {
		name          string
		setting       string
		entrypoint    bool
		file          bool
		unified       bool
		standalone    bool
		moduleUnified bool
		keepStyle     bool
		want          bool
	}{
		{name: "new entrypoint", entrypoint: true, want: true},
		{name: "existing runtime"},
		{name: "existing embedded entrypoint", entrypoint: true, file: true},
		{name: "existing unified entrypoint", entrypoint: true, file: true, unified: true, want: true},
		{name: "explicit migration", entrypoint: true, file: true, setting: "true", want: true},
		{name: "explicit embedded", entrypoint: true, file: true, unified: true, setting: "false"},
		{name: "new standalone", standalone: true, want: true},
		{name: "existing embedded standalone", standalone: true, file: true},
		{name: "existing unified standalone", standalone: true, file: true, unified: true, want: true},
		{name: "explicit standalone migration", standalone: true, file: true, setting: "true", want: true},
		{name: "explicit embedded standalone", standalone: true, file: true, unified: true, setting: "false"},
		{name: "new client beside custom runner", standalone: true, keepStyle: true},
		{name: "unified client beside custom runner", standalone: true, keepStyle: true, file: true, unified: true, want: true},
		{name: "unified module requires unified dependencies", standalone: true, moduleUnified: true, file: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting := ""
			if tc.setting != "" {
				setting = "unifiedClients: " + tc.setting
			}
			contents := "embedded bindings"
			if tc.unified {
				contents = "dagger.DefaultGraphQLClient( dagger.ModuleGraphQLClient("
			}
			runSdkSelection(t, fmt.Sprintf(`
type File { contents: String! }
type Workspace {
  present: Boolean!
  contents: String!
  pub file(path: String!): File! { File(contents: contents) }
}
type ModuleSource { moduleOriginalName: String! }
type Sdk {
%s
  let exists(ws: Workspace!, path: String!): Boolean! { ws.present }
%s
%s
  pub selected(ws: Workspace!): Boolean! {
    if (%t) { unifiedClient(ws, ModuleSource(moduleOriginalName: "dep"), %t, %t) }
    else { usesUnifiedModule(ws, %t) }
  }
}
let selected = Sdk(%s).selected(ws: Workspace(present: %t, contents: %q))
assert(%q) { selected == %t }
`, sdkSetting(t, "unifiedClients"), sdkMember(t, sdkSource(t), "usesUnifiedModule("), sdkMember(t, sdkSource(t), "unifiedClient("), tc.standalone, tc.moduleUnified, tc.keepStyle, tc.entrypoint, setting, tc.file, contents, tc.name, tc.want))
		})
	}
}
