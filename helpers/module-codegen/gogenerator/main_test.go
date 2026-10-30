package gogenerator

import (
	"os"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

// TestMain runs the tests without the Dagger session they may run in: a
// generator that needs the engine to name identifiers gets a fake one from
// its test (GenerateConfig.Connector), so no test depends on the engine
// around it.
func TestMain(m *testing.M) {
	os.Unsetenv("DAGGER_SESSION_PORT")
	os.Unsetenv("DAGGER_SESSION_TOKEN")
	os.Exit(m.Run())
}

// preCutEngine is an engine released before identifiers.CasingVersion. Its
// schemas report every v1.0.0 prerelease as v1.0.0, and it names modules by
// strcase.
func preCutEngine() *identifiers.Connector {
	return (&identifierstest.Engine{Release: "v1.0.0-beta.21"}).Connector()
}
