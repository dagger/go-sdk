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
		name    string
		setting string
		remote  string
		want    bool
	}{
		{name: "unset", setting: "", want: true},
		{name: "explicit migration", setting: "true", want: true},
		{name: "explicit runtime or custom local runner", setting: "false"},
		{name: "remote entrypoint", setting: "", remote: "github.com/example/sdk/entrypoint@v1", want: true},
		{name: "remote entrypoint with explicit false", setting: "false", remote: "github.com/example/sdk/entrypoint@v1", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting := ""
			if tc.setting != "" {
				setting = "dangEntrypoint: " + tc.setting
			}
			program := fmt.Sprintf(`
type Workspace {
  remote: String!
}
type Sdk {
  dangEntrypoint: Boolean! = true
  let remoteEntrypoint(ws: Workspace!): String! { ws.remote }
%s
  pub selected(ws: Workspace!): Boolean! {
    usesEntrypoint(ws)
  }
}
let selected = Sdk(%s).selected(ws: Workspace(remote: %q))
assert(%q) { selected == %t }
`, sdkMember(t, sdkSource(t), "usesEntrypoint("), setting, tc.remote, tc.name, tc.want)
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "sdk.dang"), []byte(program), 0o600))
			_, err := dang.RunDir(context.Background(), dir, false)
			require.NoError(t, err)
		})
	}
}
