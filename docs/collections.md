# Collections

Collection support requires [dagger/dagger#14221](https://github.com/dagger/dagger/pull/14221).

The Go runtime and the opt-in Dang entrypoint read `// +collection`, `// +keys`,
`// +get`, and `// +delta`. The entrypoint builds a generated copy of collection
source with private state, preserving the collection base through ordinary Go
value copies without editing author files. The entrypoint remains opt-in.

The standalone client generator in this repository consumes the engine's
projected schema. Collections remain object types. Clients expose `Keys`,
`Get`, `List`, and `Subset`. They also expose `Batch` when the collection has
functions other than its get function. They do not expose the author's hidden
state.

Run the client compatibility test from `cmd/dagger-go-sdk-codegen`:

```sh
go test ./generator/gogenerator/... -run TestGenerateCollectionClient
```
