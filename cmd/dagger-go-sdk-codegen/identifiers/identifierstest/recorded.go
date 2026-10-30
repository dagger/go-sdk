package identifierstest

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// recorded is engine/naming's formatting of every name the SDK's tests
// format (engine/naming in dagger/dagger 168f78aab59c, the dictionary of
// v1.0.0-beta.22). The engine has one dictionary for every version so far,
// so it is the formatting at any version.
//
//go:embed testdata/recorded.json
var recorded []byte

// Recorded returns the recorded formatting, a fresh copy each time.
func Recorded() Table {
	var table Table
	if err := json.Unmarshal(recorded, &table); err != nil {
		panic(fmt.Sprintf("decode recorded names: %v", err))
	}
	return table
}

// Styled returns an engine released at release that formats names as
// recorded. A name the recording lacks fails the query: record it with the
// engine and add it.
func Styled(release string) *Engine {
	return &Engine{Release: release, Table: Recorded()}
}
