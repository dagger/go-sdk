package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vito/dang/v2/pkg/dang"
)

func TestSdkEntrypointSelection(t *testing.T) {
	for _, tc := range []struct {
		name        string
		setting     string
		hadManifest bool
		generated   bool
		remote      string
		want        bool
	}{
		{name: "new module", setting: "null", want: true},
		{name: "new runtime module", setting: "false"},
		{name: "existing runtime or custom local runner", setting: "null", hadManifest: true},
		{name: "explicit runtime or custom local runner", setting: "false", hadManifest: true},
		{name: "explicit migration", setting: "true", hadManifest: true, want: true},
		{name: "generated entrypoint", setting: "null", hadManifest: true, generated: true, want: true},
		{name: "remote entrypoint", setting: "null", hadManifest: true, remote: "github.com/example/sdk/entrypoint@v1", want: true},
		{name: "remote entrypoint with explicit true", setting: "true", hadManifest: true, remote: "github.com/example/sdk/entrypoint@v1", want: true},
		{name: "remote entrypoint with explicit false", setting: "false", hadManifest: true, remote: "github.com/example/sdk/entrypoint@v1", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program := fmt.Sprintf(`
type Workspace {
  generated: Boolean!
  remote: String!
}
type Sdk {
  dangEntrypoint: Boolean
  fat: Boolean! = false
  template: String! = "default"
  let hasGeneratedEntrypoint(ws: Workspace!): Boolean! { ws.generated }
  let remoteEntrypoint(ws: Workspace!): String! { ws.remote }
%s
  pub selected(ws: Workspace!, hadManifest: Boolean!): Boolean! {
    usesEntrypoint(ws, hadManifest)
  }
}
let selected = Sdk(dangEntrypoint: %s).selected(
  ws: Workspace(generated: %t, remote: %q), hadManifest: %t,
)
assert(%q) { selected == %t }
`, sdkMember(t, sdkSource(t), "usesEntrypoint("), tc.setting, tc.generated, tc.remote, tc.hadManifest, tc.name, tc.want)
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "sdk.dang"), []byte(program), 0o600))
			_, err := dang.RunDir(context.Background(), dir, false)
			require.NoError(t, err)
		})
	}
}
