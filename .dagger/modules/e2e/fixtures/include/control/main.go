// Package include_control reads nothing outside its directory.
package include_control

import (
	"strconv"
	"time"
)

type IncludeControl struct{}

// Nonce returns the time the function ran.
func (*IncludeControl) Nonce() string { return strconv.FormatInt(time.Now().UnixNano(), 10) }
