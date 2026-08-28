package main

type TomlNestedApp struct{}

// Where the module's source lives, relative to its root.
func (m *TomlNestedApp) SourceDir() string {
	return "src"
}
