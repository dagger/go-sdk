package greet

import _ "embed"

//go:embed greeting.txt
var greeting string

func Greeting() string { return greeting }
