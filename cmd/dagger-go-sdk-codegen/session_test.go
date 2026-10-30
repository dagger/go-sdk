package main

import (
	"os"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

// TestMain runs the commands against a fake engine (identifierstest.Query),
// never the Dagger session the tests may run in.
func TestMain(m *testing.M) {
	os.Unsetenv("DAGGER_SESSION_PORT")
	os.Unsetenv("DAGGER_SESSION_TOKEN")
	sessionQuery = func() (identifiers.Query, error) { return identifierstest.Query, nil }
	os.Exit(m.Run())
}
