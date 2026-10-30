package main

import (
	"os"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

// TestMain runs the commands against a fake engine at the Go SDK's floor,
// which styles Go identifiers as recorded, and never the Dagger session the
// tests may run in. A test of the engine's naming rules replaces
// sessionConnector.
func TestMain(m *testing.M) {
	os.Unsetenv("DAGGER_SESSION_PORT")
	os.Unsetenv("DAGGER_SESSION_TOKEN")
	sessionConnector = func() *identifiers.Connector {
		return identifierstest.Styled(identifiers.CasingVersion).Connector()
	}
	os.Exit(m.Run())
}
