package main

import (
	"os"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

// TestMain runs the commands against an engine released before
// identifiers.CasingVersion, like the engines the fixtures come from (they
// report every v1.0.0 prerelease as v1.0.0), which styles Go identifiers as
// recorded, and never the Dagger session the tests may run in. A test of the
// engine's naming rules replaces sessionConnector.
func TestMain(m *testing.M) {
	os.Unsetenv("DAGGER_SESSION_PORT")
	os.Unsetenv("DAGGER_SESSION_TOKEN")
	sessionConnector = func() *identifiers.Connector {
		return identifierstest.Styled("v1.0.0-beta.21").Connector()
	}
	os.Exit(m.Run())
}
