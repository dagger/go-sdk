package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vito/dang/v2/pkg/dang"
)

type precedence struct {
	a, b string
	want bool
}

// runReleasePrecedes evaluates go-sdk.dang's comparator with the Dang release
// the engine embeds, and asserts each case in Dang.
func runReleasePrecedes(t *testing.T, cases []precedence) error {
	t.Helper()
	source := sdkSource(t)
	var program strings.Builder
	program.WriteString("type Versions {\n  pub precedes(a: String!, b: String!): Boolean! { releasePrecedes(a, b) }\n\n")
	program.WriteString(sdkMember(t, source, "releasePattern"))
	program.WriteString(sdkMember(t, source, "releasePrecedes("))
	program.WriteString("\n")
	program.WriteString(sdkMember(t, source, "identifiersPrecede("))
	program.WriteString("}\n\nlet versions = Versions()\n")
	for _, c := range cases {
		fmt.Fprintf(&program, "assert(%q) { versions.precedes(%q, %q) == %t }\n", fmt.Sprintf("%s precedes %s is %t", c.a, c.b, c.want), c.a, c.b, c.want)
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "versions.dang"), []byte(program.String()), 0o600))
	_, err := dang.RunDir(context.Background(), dir, false)
	return err
}

func TestSdkReleasePrecedes(t *testing.T) {
	require.NoError(t, runReleasePrecedes(t, []precedence{
		{"v1.0.0-beta.21", "v1.0.0-beta.22", true},
		{"v1.0.0-beta.22", "v1.0.0-beta.21", false},
		{"v1.0.0-beta.21", "v1.0.0-beta.21", false},
		{"v1.0.0-beta.9", "v1.0.0-beta.10", true},
		{"v1.0.0-beta.10", "v1.0.0-beta.9", false},
		{"v1.0.0-beta.999999999999999999999", "v1.0.0-beta.1000000000000000000000", true},
		{"v1.0.0-beta.1000000000000000000000", "v1.0.0-beta.999999999999999999999", false},
		{"v1.0.0-rc-1", "v1.0.0", true},
		{"v1.0.0-dev.1", "v1.0.0", true},
		{"v1.0.0-beta.20+dev", "v1.0.0-beta.20", false},
		{"v1.0.0-beta.20", "v1.0.0-beta.20+dev", false},
		{"v1.0.0-beta.18+dev", "v1.0.0-beta.21", true},
		{"1.0.0-beta.21", "v1.0.0-beta.22", true},
		{"devel", "v1.0.0-beta.21", false},
		{"v1.0.0-beta.21", "devel", false},
		{"v1.0.0-beta..1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0-beta..1", false},
		{"v1.0.0-", "v1.0.0", false},
		{"v1.0.0", "v1.0.0-", false},
		{"v1.0.0-.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0-.1", false},
		{"v1.0.0-beta.1+", "v1.0.0", false},
		{"v0.1.0+???", "v1.0.0-beta.21", false},
		{"v1.0.0-beta.21", "v0.1.0+???", false},
		{"v0.1.0-..", "v1.0.0", false},
		{"v1.0.0", "v0.1.0-..", false},
		{"v0.1.0+a..b", "v1.0.0", false},
		{"v1.0.0", "v0.1.0+a..b", false},
		{"v1.0.0-beta.21", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-beta.21", false},
		{"v1.0.0-beta.21+a7a2e64e", "v1.0.0-beta.21", false},
		{"v1.0.0-beta.21", "v1.0.0-beta.21+a7a2e64e", false},
		{"v1.0.0-beta.21+a7a2e64e", "v1.0.0-beta.22", true},
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.9", "v1.0.10", true},
		{"v0.9.0", "v1.0.0-beta.1", true},
		{"v1.0.0-alpha.1", "v1.0.0-beta.1", true},
		{"v1.0.0-beta.1", "v1.0.0-beta.1.1", true},
		{"v1.0.0-rc.1", "v1.0.0-beta.22", false},
		{"v1.0.0-1", "v1.0.0-beta", true},
		{"dev", "v1.0.0-beta.21", false},
		{"v1.0.0-beta.21", "dev", false},
		{"", "v1.0.0-beta.22", false},
	}))
}

// A wrong expectation fails, so the assertions above run.
func TestSdkReleasePrecedesAsserts(t *testing.T) {
	err := runReleasePrecedes(t, []precedence{{"v1.0.0-beta.22", "v1.0.0-beta.21", true}})
	require.ErrorContains(t, err, "v1.0.0-beta.22 precedes v1.0.0-beta.21 is true")
}
