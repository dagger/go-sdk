package gogenerator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

	"codegen/generator"
)

func moduleGenerator(t *testing.T, libVersion string) GoGenerator {
	t.Helper()
	return GoGenerator{
		Config: generator.Config{
			OutputDir: t.TempDir(),
			ModuleConfig: &generator.ModuleGeneratorConfig{
				ModuleName:       "test",
				ModuleSourcePath: ".",
				LibVersion:       libVersion,
			},
		},
	}
}

func commandLines(genSt *generator.GeneratedState) []string {
	var lines []string
	for _, cmd := range genSt.PostCommands {
		lines = append(lines, strings.Join(cmd.Args, " "))
	}
	return lines
}

func TestSyncModReplaceAndTidyPinsDaggerWithoutUpdatingTransitiveDeps(t *testing.T) {
	mod, err := modfile.Parse("go.mod", []byte("module example.com/test\n\ngo 1.25.0\n"), nil)
	require.NoError(t, err)

	genSt := &generator.GeneratedState{}
	g := moduleGenerator(t, "v1.2.3")
	require.NoError(t, g.syncModReplaceAndTidy(mod, genSt, ".", nil))

	require.Equal(t, []string{"go get dagger.io/dagger@v1.2.3", "go mod tidy"}, commandLines(genSt))
}

func TestSyncModReplaceAndTidyKeepsCustomDaggerReplace(t *testing.T) {
	mod, err := modfile.Parse("go.mod", []byte("module example.com/test\n\ngo 1.25.0\n\nreplace dagger.io/dagger => ../sdk\n"), nil)
	require.NoError(t, err)

	genSt := &generator.GeneratedState{}
	g := moduleGenerator(t, "v1.2.3")
	require.NoError(t, g.syncModReplaceAndTidy(mod, genSt, ".", nil))

	require.Equal(t, []string{"go mod tidy"}, commandLines(genSt))
}

// The pinned library's go.mod seeds minimum versions and, for paths the
// module already requires, its replace directives — the engine's behaviour
// with its embedded SDK go.mod.
func TestSyncModReplaceAndTidySeedsFromLibrary(t *testing.T) {
	mod, err := modfile.Parse("go.mod", []byte(`module example.com/test

go 1.25.0

require (
	example.com/old v1.0.0
	example.com/newer v1.9.0
	go.opentelemetry.io/otel/log v0.20.0
)
`), nil)
	require.NoError(t, err)

	sdkMod, err := modfile.Parse("go.mod", []byte(`module dagger.io/dagger

go 1.26.1

require (
	example.com/old v1.5.0
	example.com/newer v1.0.0
	example.com/unused v1.3.0 // indirect
)

replace (
	go.opentelemetry.io/otel/log => go.opentelemetry.io/otel/log v0.16.0
	go.opentelemetry.io/otel/sdk/log => go.opentelemetry.io/otel/sdk/log v0.16.0
)
`), nil)
	require.NoError(t, err)

	genSt := &generator.GeneratedState{}
	g := moduleGenerator(t, "v1.2.3")
	require.NoError(t, g.syncModReplaceAndTidy(mod, genSt, ".", sdkMod))

	versions := map[string]string{}
	for _, req := range mod.Require {
		versions[req.Mod.Path] = req.Mod.Version
	}
	require.Equal(t, "v1.5.0", versions["example.com/old"], "older requirement is raised to the library's minimum")
	require.Equal(t, "v1.9.0", versions["example.com/newer"], "newer requirement is kept")
	require.Equal(t, "v1.3.0", versions["example.com/unused"], "library requirements are seeded; go mod tidy prunes them")

	require.Equal(t, []string{
		"go mod edit -replace go.opentelemetry.io/otel/log=go.opentelemetry.io/otel/log@v0.16.0",
		"go get dagger.io/dagger@v1.2.3",
		"go mod tidy",
	}, commandLines(genSt), "only replaces for paths the module requires are copied")
}

func TestSupportsSelfTypes(t *testing.T) {
	require.True(t, supportsSelfTypes("v1.0.0"))
	require.True(t, supportsSelfTypes("v0.12.0"))
	require.False(t, supportsSelfTypes("v0.11.9"))
	require.False(t, supportsSelfTypes(""))
}
