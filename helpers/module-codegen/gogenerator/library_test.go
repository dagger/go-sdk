package gogenerator

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/library"
)

// postCutLibrary makes dagger.io/dagger resolvable at versions, releases at
// or past identifiers.CasingVersion, whose core unified bindings alias. A
// published release resolves as it is. GO_SDK_TEST_RUNTIME, a checkout of
// dagger.io/dagger at or past the cut (dagger/dagger's sdk/go), stands in for
// every version instead, served from a file GOPROXY in front of the usual
// one, with a module cache of its own so the stand-in never reaches the real
// one. Without it, a test that needs an unpublished release skips.
func postCutLibrary(t *testing.T, versions ...string) {
	t.Helper()
	sdk := os.Getenv("GO_SDK_TEST_RUNTIME")
	if sdk == "" {
		dir := t.TempDir()
		for _, version := range versions {
			if problem := library.Problem(dir, version); problem != "" {
				t.Skipf("%s %s is not published (%s): set GO_SDK_TEST_RUNTIME to a checkout of it at or past %s to stand in for it", library.Path, version, problem, identifiers.CasingVersion)
			}
		}
		return
	}
	proxy := t.TempDir()
	dir := filepath.Join(proxy, filepath.FromSlash(library.Path), "@v")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	goMod, err := os.ReadFile(filepath.Join(sdk, "go.mod"))
	require.NoError(t, err)
	for _, version := range versions {
		require.NoError(t, os.WriteFile(filepath.Join(dir, version+".info"), []byte(`{"Version":"`+version+`","Time":"2026-01-01T00:00:00Z"}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, version+".mod"), goMod, 0o644))
		zip, err := os.Create(filepath.Join(dir, version+".zip"))
		require.NoError(t, err)
		require.NoError(t, modzip.CreateFromDir(zip, module.Version{Path: library.Path, Version: version}, sdk))
		require.NoError(t, zip.Close())
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "list"), []byte(strings.Join(versions, "\n")+"\n"), 0o644))
	upstream, err := exec.Command("go", "env", "GOPROXY").Output()
	require.NoError(t, err)
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}).String()+","+strings.TrimSpace(string(upstream)))
	t.Setenv("GONOSUMDB", library.Path)
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOMODCACHE", standInModCache(t))
}

var (
	standInCacheOnce sync.Once
	standInCache     string
	standInCacheErr  error
)

// standInModCache is the module cache of the tests a stand-in library
// serves, shared by the test binary and removed when it exits (TestMain).
func standInModCache(t *testing.T) string {
	standInCacheOnce.Do(func() {
		standInCache, standInCacheErr = os.MkdirTemp("", "go-sdk-stand-in-modcache-")
	})
	require.NoError(t, standInCacheErr)
	return standInCache
}
