package main

import "fmt"

type RuntimeApp struct{}

// Greet someone.
func (m *RuntimeApp) Greeting(name string) string {
	return fmt.Sprintf("hello %s, served by the go-sdk runtime", name)
}
