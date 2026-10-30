package gogenerator

import (
	"os"
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/identifiers/identifierstest"
)

// TestMain runs generation against a fake engine (identifierstest.Query),
// never the Dagger session the tests may run in. It names a module by its
// source ID, so tests set ModuleSourceID to the module's name.
func TestMain(m *testing.M) {
	os.Unsetenv("DAGGER_SESSION_PORT")
	os.Unsetenv("DAGGER_SESSION_TOKEN")
	sessionQuery = func() (identifiers.Query, error) { return identifierstest.Query, nil }
	os.Exit(m.Run())
}
