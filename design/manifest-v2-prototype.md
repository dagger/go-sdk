# Go SDK manifest-v2 prototype

This branch moves Go module generation from the engine into the Go SDK module.
The SDK module remains loadable through `dagger.json` and engine version
`v1.0.0-beta.11`. Generated v2 manifests do not contain that legacy engine
version.

## Reuse boundary

The client renderer, Go source analyzer, type validation, JSON codec, static
dispatch, and self-call generation share one package:
`helpers/codegen/generator/gogenerator/templates`.

The analyzer is a direct port of the hardened analyzer from
`github.com/dagger/dagger/cmd/codegen/generator/go/templates` at Dagger commit
`d9071a13e`. It retains:

- Go package loading and source maps
- object, interface, enum, list, optional, and scalar analysis
- constructor and function validation
- pragma parsing
- JSON codecs for Dagger values
- static invocation cases
- module self-introspection for self-call client generation

The new `GenerateEmbeddedClient` mode writes the existing client below
`internal/dagger` without a nested `go.mod`. The v2 renderer adds:

- an exported immutable `DaggerDispatch` function
- `cmd/<module>-dispatch` with `engine-call` and `call` modes
- `internal/dagger/entrypoint/main.dang`
- `dagger-module.toml` manifest version 2

`helpers/module-codegen` only coordinates these shared parts. Its schema merge
is a direct port of `core.Schema.Merge`. It generates self-call bindings
without an engine schema helper call.

## Generation flow

1. Generate the dependency client with the existing client renderer.
2. Load and analyze the importable module package.
3. Emit the module schema and merge it with the dependency schema.
4. Generate the client again with self-call bindings.
5. Reload the package and emit codecs plus static dispatch.
6. Emit the Dang entrypoint, dispatch command, and v2 manifest.
7. Type-check the Dang entrypoint against `ModuleEntrypoint` and the selected
   Dagger schema.

The generated entrypoint finds `go.mod` above the module directory. It mounts
only that Go root, uses a stable build cache, and builds the dispatch command
in the module subdirectory. Call values affect command execution only.

The functional builder in `helpers/module-codegen/manifest` writes the exact
v2 manifest fields. It rejects missing entrypoint values and unsupported
entrypoint kinds.

## Current limits

- The engine-side manifest-v2 loader is not complete. Normal module loading
  cannot yet test source resolution, type binding, constructor selection,
  result conversion, cache policy, `currentNode`, recursive module drivers, or
  legacy-loader selection.
- A v2 Go module must use an importable root package. `package main` is
  rejected with a migration message.
- The generated process transport is private to this SDK prototype.

## Local validation

```console
(cd helpers/codegen && go test ./...)
(cd helpers/module-codegen && go test ./...)
(cd helpers/render-template && go test ./...)
(cd helpers/entrypoint-contract && go test ./...)

dagger call e-2-e helper-tests-check
dagger call e-2-e generate-module-check
dagger check e-2-e
```

The generated fixture compiles and supports shell and engine dispatch. Tests
cover constructors, receiver state, strings, lists, optional values, enums,
void results, errors, self-call bindings, nested Dagger calls, and core object
ID round trips.
