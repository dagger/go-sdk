package gogenerator

import (
	"os"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

// TestMain runs the tests without the Dagger session they may run in: a
// generator gets a fake engine, from its test (GenerateConfig.Connector) or
// preCutEngine, so no test depends on the engine around it.
func TestMain(m *testing.M) {
	os.Unsetenv("DAGGER_SESSION_PORT")
	os.Unsetenv("DAGGER_SESSION_TOKEN")
	sessionConnector = preCutEngine
	code := m.Run()
	if standInCache != "" {
		os.RemoveAll(standInCache)
	}
	os.Exit(code)
}

// preCutEngine is an engine released before identifiers.CasingVersion. Its
// schemas report every v1.0.0 prerelease as v1.0.0. It styles Go identifiers
// as recorded (identifierstest.Recorded), and names the module sources
// "hello", "hello-world" and "app" by strcase, as the engine does a module
// served before the cut.
func preCutEngine() *identifiers.Connector {
	engine := identifierstest.Styled("v1.0.0-beta.21")
	engine.Modules = map[string]*identifierstest.Module{}
	for _, name := range []string{"hello", "hello-world", "app"} {
		engine.Modules[name] = identifierstest.Strcase(name)
	}
	return engine.Connector()
}
