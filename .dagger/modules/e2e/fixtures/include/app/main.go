// Package include_app builds with a package of a sibling Go module.
package include_app

import (
	"os"

	"example.com/include-app/internal/dagger"
	"strconv"
	"time"

	"example.com/lib/greet"
)

type IncludeApp struct{}

// Greeting returns the text that lib/greet embeds when the module is built.
func (*IncludeApp) Greeting(ws *dagger.Workspace) string { return greet.Greeting() }

// Runtime reads the same file when the function runs.
func (*IncludeApp) Runtime(ws *dagger.Workspace) (string, error) {
	data, err := os.ReadFile("../lib/greet/greeting.txt")
	return string(data), err
}

// Excluded reports whether lib/greet/testdata, which the build does not read,
// reached the module.
func (*IncludeApp) Excluded(ws *dagger.Workspace) bool {
	_, err := os.Stat("../lib/greet/testdata")
	return err == nil
}

// Nonce returns the greeting and the time the function ran.
func (*IncludeApp) Nonce(ws *dagger.Workspace) string {
	return greet.Greeting() + " " + strconv.FormatInt(time.Now().UnixNano(), 10)
}
