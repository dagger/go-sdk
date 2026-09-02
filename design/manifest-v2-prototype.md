# Go SDK manifest-v2 prototype

This branch moves Go module generation from the engine into the Go SDK module.
The SDK module remains loadable through `dagger.json` and engine version
`v1.0.0-beta.11`.

## Reuse boundary

The module client uses the existing generator in `helpers/codegen`. The new
`GenerateEmbeddedClient` mode writes that client below `internal/dagger`
without creating a nested `go.mod`.

The module analyzer in `helpers/module-codegen/gogenerator/templates` is a
direct port of the hardened analyzer from
`github.com/dagger/dagger/cmd/codegen/generator/go/templates` at Dagger commit
`d9071a13e`. It retains:

- Go package loading and source maps
- object, interface, enum, list, optional, and scalar analysis
- constructor and function validation
- pragma parsing
- JSON codecs for Dagger values
- static invocation cases
- module self-introspection for self-call client generation

`helpers/module-codegen/gogenerator/templates/v2.go` is the new boundary. It
replaces the legacy ambient `currentFunctionCall` entrypoint with:

- an exported immutable `DaggerDispatch` function
- `cmd/<module>-dispatch` with `engine-call` and `call` modes
- `internal/dagger/entrypoint/main.dang`
- `dagger-module.toml` manifest version 2

The userland schema merge in `schema_merge.go` is a direct port of
`core.Schema.Merge`. It lets the SDK generate self-call bindings without an
engine schema helper call.

## Generation flow

1. Generate the dependency client with the existing client renderer.
2. Load and analyze the importable module package.
3. Emit the module's schema and merge it with the dependency schema.
4. Generate the client again with self-call bindings.
5. Reload the package and emit codecs plus static dispatch.
6. Emit the Dang entrypoint, dispatch command, and v2 manifest.

The generated Dang entrypoint builds the Go dispatch command from the module
workspace. Call values affect only command execution. They do not affect the
build step.

## Current limits

- The engine-side manifest-v2 loader is not complete, so the generated
  entrypoint cannot yet be tested through normal module loading.
- A v2 Go module must use an importable root package. `package main` is
  rejected with a migration message.
- The generated entrypoint returns `null` from `main`. It uses the specified
  default-main-object behavior.
- The process transport is private to this SDK prototype.

## Local validation

```console
cd helpers/codegen
go test ./generator/... ./introspection/...

cd ../module-codegen
go test ./...
```

The module helper test fixture can also be generated into a temporary copy.
The resulting Go module supports these checks:

```console
go test ./...
go run ./cmd/hello-dispatch call greet \
  --receiver-json '{"Prefix":"Hi"}' --name World
```
