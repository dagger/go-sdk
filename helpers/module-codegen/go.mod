module module-codegen

go 1.25.1

require (
	codegen v0.0.0
	github.com/BurntSushi/toml v1.6.0
	github.com/iancoleman/strcase v0.3.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/mod v0.37.0
	golang.org/x/tools v0.45.0
)

require (
	github.com/dave/jennifer v1.7.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/psanford/memfs v0.0.0-20241019191636-4ef911798f9b // indirect
	golang.org/x/sync v0.20.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace codegen => ../codegen
