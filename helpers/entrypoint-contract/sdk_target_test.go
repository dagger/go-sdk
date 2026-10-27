package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vito/dang/v2/pkg/dang"
)

func sdkSource(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "go-sdk.dang"))
	require.NoError(t, err)
	return string(source)
}

// sdkMember returns a private member of go-sdk.dang's GoSdk type: a block
// member through its closing brace, or a one-line field.
func sdkMember(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "\n  let "+name)
	require.Positive(t, start, "go-sdk.dang has no member "+name)
	rest := source[start+1:]
	line := rest[:strings.Index(rest, "\n")+1]
	if !strings.HasSuffix(strings.TrimSpace(line), "{") {
		return line
	}
	end := strings.Index(rest, "\n  }\n")
	require.Positive(t, end, name)
	return rest[:end+len("\n  }\n")]
}

// resolveSdkTarget evaluates go-sdk.dang's target resolution with the Dang
// release the engine embeds, for the given setting. The program asserts in
// Dang that the SDK resolves want, so a refusal or another value is the
// returned error.
func resolveSdkTarget(t *testing.T, setting, want string) error {
	t.Helper()
	source := sdkSource(t)
	var program strings.Builder
	program.WriteString("type Sdk {\n  targetVersion: String!\n\n")
	for _, name := range []string{"sourcesFloor", "defaultTarget", "releasePattern", "release(", "isRelease(", "releasePrecedes(", "identifiersPrecede(", "targetRelease("} {
		program.WriteString(sdkMember(t, source, name))
		program.WriteString("\n")
	}
	program.WriteString("  pub resolved: String! { targetRelease(\"module \\\"probe\\\"\") }\n}\n\n")
	fmt.Fprintf(&program, "let target = Sdk(targetVersion: %q).resolved\n", setting)
	fmt.Fprintf(&program, "assert(%q) { target == %q }\n", "resolved "+want, want)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sdk.dang"), []byte(program.String()), 0o600))
	_, err := dang.RunDir(context.Background(), dir, false)
	return err
}

func TestSdkTargetRelease(t *testing.T) {
	for _, c := range []struct{ setting, want string }{
		{"", "v1.0.0-beta.21"},
		{"v1.0.0-beta.21", "v1.0.0-beta.21"},
		{"1.0.0-beta.21+abc", "v1.0.0-beta.21"},
		{"v1.0.0-beta.20", "v1.0.0-beta.20"},
		{"1.0.0-beta.18", "v1.0.0-beta.18"},
	} {
		require.NoError(t, resolveSdkTarget(t, c.setting, c.want), "%+v", c)
	}
}

// A wrong expectation fails, so the assertions above run.
func TestSdkTargetReleaseAsserts(t *testing.T) {
	err := resolveSdkTarget(t, "", "v1.0.0-beta.20")
	require.ErrorContains(t, err, "resolved v1.0.0-beta.20")
}

func TestSdkTargetReleaseRefusals(t *testing.T) {
	const fix = "Set targetVersion between v1.0.0-beta.18 and v1.0.0-beta.21 in the scope's settings in dagger.toml, or remove it"
	const below = `module "probe" pins targetVersion v1.0.0-beta.17, which this release of the Go SDK cannot generate for: it is below v1.0.0-beta.18, the oldest engine the generated entrypoint loads on. ` + fix
	above := func(v string) string {
		return `module "probe" pins targetVersion ` + v + ", which this release of the Go SDK cannot generate for: it targets v1.0.0-beta.21 at most. " + fix + ", or install the SDK release built for " + v
	}
	malformed := func(v string) string {
		return `module "probe" pins targetVersion "` + v + `", which is not a Dagger release such as v1.0.0-beta.21. ` + fix
	}
	for _, c := range []struct{ setting, want string }{
		{"v1.0.0-beta.17", below},
		{"1.0.0-beta.17+abc", below},
		{"v1.0.0-beta.22", above("v1.0.0-beta.22")},
		{"99.0.0", above("v99.0.0")},
		{"v1.0.0", above("v1.0.0")},
		{"latest", malformed("latest")},
		{"v1.0.0-beta..1", malformed("v1.0.0-beta..1")},
		{"v1.0.0-beta.21+???", malformed("v1.0.0-beta.21+???")},
	} {
		require.ErrorContains(t, resolveSdkTarget(t, c.setting, ""), c.want, "%+v", c)
	}
}

// The SDK's release is a published dagger.io/dagger, the library a module
// pins by default, and the setting's documentation names the same release.
func TestSdkDefaultTargetIsPublished(t *testing.T) {
	source := sdkSource(t)
	match := regexp.MustCompile(`let defaultTarget: String! = "([^"]*)"`).FindStringSubmatch(source)
	require.NotNil(t, match, "go-sdk.dang has no defaultTarget")
	release := match[1]
	require.Regexp(t, `^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`, release)
	require.Contains(t, source, "the release this Go SDK is built for, "+release+".")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/probe\n\ngo 1.26.1\n"), 0o600))
	cmd := exec.Command("go", "list", "-m", "-e", "-f", "{{with .Error}}{{.Err}}{{end}}", "dagger.io/dagger@"+release)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Empty(t, strings.TrimSpace(string(out)), "dagger.io/dagger %s is not published", release)
}

// The SDK's Dang sources and the e2e checks parse; the engine type-checks
// them against its schema only when it loads them.
func TestDangSourcesParse(t *testing.T) {
	files := []string{filepath.Join("..", "..", "go-sdk.dang")}
	e2e, err := filepath.Glob(filepath.Join("..", "..", ".dagger", "modules", "*", "*.dang"))
	require.NoError(t, err)
	for _, path := range append(files, e2e...) {
		_, err := dang.ParseFile(path)
		require.NoError(t, err, path)
	}
}
