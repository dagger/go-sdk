# Collections

Collection support requires [dagger/dagger#14221](https://github.com/dagger/dagger/pull/14221).

Go module generation and execution use the engine's Go runtime. That runtime
reads `// +collection`, `// +keys`, `// +get`, and `// +delta`. It also preserves
the internal collection base through ordinary Go value copies.

The standalone client generator in this repository consumes the engine's
projected schema. Collections remain object types. Clients expose `Keys`,
`Get`, `List`, `Subset`, and `Batch`; they do not expose the author's hidden state.

Run the client compatibility test from `helpers/codegen`:

```sh
go test ./generator/gogenerator/... -run TestGenerateCollectionClient
```
