# Go SDK manifest v2 prototype

The `dangEntrypoint` setting generates Go modules that the engine loads through
a manifest v2 Dang entrypoint. The engine reads the module's types from the
entrypoint and does not run a Go runtime container to register them. The
setting is off by default. Without it, the SDK generates modules for the Go
runtime exactly as before.

Manifest v2 loading shipped in Dagger v1.0.0-beta.14 (dagger/dagger#14038). A
module generated with `dangEntrypoint` needs that engine or a later one.

## Enabling it

The engine turns the setting into an SDK flag:

```console
dagger module init go --name hello --path mods/hello --dang-entrypoint
```

The engine stores the setting on the scope in `dagger.toml`:

```toml
[sdks.go.scopes."mods/hello".settings]
dangEntrypoint = true
```

`dagger generate` then regenerates the module with the same setting.
`--dang-entrypoint=false` turns it off. The Python SDK has a setting with the
same name and the same flag.

## Generated module

```text
hello/
├── dagger-module.toml
├── go.mod
├── go.sum
├── main.go
├── .dagger-generated.json
├── dagger.gen.go
├── cmd/                         # importable package only
│   └── hello-dispatch/
│       └── main.go
├── dagger.dispatch.gen.go       # package main only
└── internal/
    └── dagger/
        ├── dagger.gen.go
        ├── hello.gen.go
        └── entrypoint/
            └── main.dang
```

| Path | Owner | Purpose |
| --- | --- | --- |
| `main.go` | Developer | Module package, either importable or `package main`. |
| `.dagger-generated.json` | Go SDK | Hashes of generated files used to preserve developer edits. |
| `dagger.gen.go` | Go SDK | Codecs and the static `DaggerDispatch` function. |
| `cmd/hello-dispatch/main.go` | Go SDK | Dispatch command for an importable package. |
| `dagger.dispatch.gen.go` | Go SDK | Dispatch command for `package main`. |
| `internal/dagger/*.gen.go` | Go SDK | Embedded Dagger client, including self-call bindings. |
| `internal/dagger/entrypoint/main.dang` | Go SDK | Entrypoint that the engine loads. |

The module may be an importable Go package or `package main`. A new module's
starter uses the module name as its package name, for example `package hello`.
Existing `package main` modules keep their package and receive an in-package
dispatch command.

The manifest names only the module and the entrypoint:

```toml
name = "hello"

[entrypoint]
  kind = "dang"
  source = "./internal/dagger/entrypoint"
```

The manifest has no `manifestVersion` key and no `engineVersion` key. The
`[entrypoint]` table selects manifest v2, and the engine rejects
`manifestVersion` as an unknown key. sdk-helpers writes the manifest, so the
SDK does not render TOML itself. The SDK removes `dagger.json`.

## Engine contract

The entrypoint implements the engine's interface:

```dang
interface ModuleEntrypoint {
  types(workspace: Workspace!): [TypeDef!]!
  call(
    workspace: Workspace!
    receiverType: String!
    receiverValue: JSON
    fnName: String!
    fnArgs: JSON!
  ): JSON!
}
```

`types` returns one type definition per Go object, interface, and enum. A
default value is JSON text: `defaultValue: ("\"alpine:3.21\"" :: Dagger.JSON!)`.
`JSON.decode` would pass the decoded value instead, and the engine would fail
to read it.

`call` builds the dispatch command and runs it with privileged nesting. It
passes one JSON request on standard input:

```json
{
  "fnArgs": "{\"name\":\"World\"}",
  "fnName": "Greet",
  "receiverType": "Hello",
  "receiverValue": "{\"Prefix\":\"Hello\"}"
}
```

`fnArgs` is one JSON object keyed by the original argument names.
`JSON.encode` writes each `JSON` value as a string of JSON text. The command
decodes each string once, and it also accepts a JSON object or null. The
constructor call has an empty `fnName` and a null `receiverValue`. The command
writes one JSON result to standard output. A Go error exits with a nonzero
status and writes the error to standard error. `call` returns the output as
`(result :: JSON!)`.

The entrypoint finds `go.mod` above the module through the workspace. It
mounts only that Go root and builds `./cmd/<module>-dispatch` in the module
subdirectory. Before it builds, it checks that the workspace directory holds
`cmd/<module>-dispatch/main.go` and a `dagger-module.toml` with the module's
name. The engine sets that directory to the module only for a module in the
workspace, so the check fails for git and directory module sources.

The command also has a developer mode:

```console
$ go run ./cmd/hello-dispatch call greet --name World --receiver-json '{"Prefix":"Hey"}'
Hey, World
```

Developer mode calls a function on a zero receiver unless `--receiver-json`
sets one. It does not run the constructor first. A function that calls the
Dagger API needs a session, for example `dagger run go run ...`.

## Generation flow

`generateScope` runs these steps when `dangEntrypoint` is true:

1. Refuse what a manifest with an entrypoint cannot carry.
2. Render the starter for a new module, with an importable package name. The
   starter imports the embedded client through the nearest `go.mod`, or
   through `dagger/<module>` when there is none, as the generator does.
   Existing `package main` modules remain supported.
3. Stage a runtime manifest from sdk-helpers, and read the schema and engine
   version from that staged module. The SDK does not reuse an existing
   entrypoint manifest here. With an entrypoint and no dependencies, the engine
   would load the module through the entrypoint that is being generated.
4. Run `module-codegen` on the module's Go root:
   1. Generate the dependency client with the existing client renderer.
   2. Load and analyze the module package.
   3. Emit the module schema and merge it with the dependency schema.
   4. Generate the client again with self-call bindings.
   5. Reload the package and emit codecs, static dispatch, the dispatch
      command, and the Dang entrypoint.
5. Type-check the entrypoint with `entrypoint-contract`, which installs the
   engine's `ModuleEntrypoint` interface next to it.
6. Merge only the generated files, ownership metadata, `go.mod`, and `go.sum`
   into the workspace. The generator refuses to replace an owned file that a
   developer edited. It lists unchanged generated bindings it removed, and the
   SDK removes them from the workspace too.
7. Write the manifest with sdk-helpers, and remove `dagger.json`.

The staged runtime manifest does not reach the result. When a generator step
fails, the SDK raises that step's own error message.

## Refusals

With `dangEntrypoint`, `generateScope` refuses these inputs before it writes
anything:

- Module clients. A manifest with an entrypoint has no dependency list.
- The `legacy` template for a new module.
- `fat`. A fat `dagger.json` points older engines at the Go runtime, which
  cannot load an importable package.
- Manifest settings that the new manifest would drop: `include`,
  `disableDefaultFunctionCaching`, a `source` other than `.`, the `codegen`,
  `clients`, and `dependencies` tables, and a runtime other than Go. The SDK
  reads `dagger-module.toml` and `dagger.json` for them.
- A file at a generated path that is not generated code:
  `dagger.gen.go`, `cmd/<module>-dispatch/main.go`, or
  `internal/dagger/entrypoint/main.dang`. The generator writes these files
  only when they are missing or start with a generated-code header.
- A change to a file recorded in `.dagger-generated.json`. The generator
  compares its saved hash and leaves the module unchanged instead of replacing
  the developer's edit.

Without `dangEntrypoint`, `generateScope` refuses a module whose manifest names
a Dang entrypoint whose source is not clearly a remote module. It parses the
manifest as TOML, so any quote style or table form counts. The Go runtime needs `package main`, so the SDK does
not switch such a module back silently. The message says to set
`dangEntrypoint = true`, or to change the package to `main` and remove the
`[entrypoint]` table.

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

`GenerateEmbeddedClient` writes the client below `internal/dagger` without a
nested `go.mod`. The embedded client returns a nullable object with a context
and an error, as the engine's Go SDK does. Standalone clients keep their
current API, and their output does not change.

`helpers/module-codegen` only coordinates these shared parts. Its schema merge
is a direct port of `core.Schema.Merge`. It generates self-call bindings
without an engine schema helper call.

## Current limits

- The engine rejects enum values on this path. It resolves a module's own enum
  as a dependency enum, so it expects GraphQL member names such as `READY`.
  The Go codec sends and expects original names such as `Ready`, as it does on
  the Go runtime path, where the same module works. A function that returns an
  enum fails with `invalid enum member "Ready"`. A function that takes an enum
  receives `"READY"` and fails to decode it. The fix belongs in the engine's
  entrypoint loader.
- A module cannot use other modules, because manifest v2 has no dependency
  list.
- The engine can load such a module only from the local workspace. For a git
  or directory module source, the engine passes the caller's workspace, which
  does not hold the module's files. The entrypoint still returns the module's
  types, but a call fails with `the Go Dang entrypoint can only load a module
  from the local workspace; git and directory module sources are not
  supported yet`. The fix belongs in the engine's entrypoint contract.
- The dispatch request format is private to this SDK.

## Verification

These checks ran against Dagger v1.0.0-beta.14 (`06660c3c`):

- `go test ./...` passes in `helpers/codegen`, `helpers/module-codegen`,
  `helpers/render-template`, and `helpers/entrypoint-contract`.
- The standalone client generator gives byte-identical output to `main` for
  the same schema and metadata. The Go runtime templates render byte-identical
  starters.
- The e2e module passes on the host engine. It checks the manifest, the
  entrypoint, the dispatch command in both modes, a module below the nearest
  `go.mod`, stable regeneration, and each refusal.
- In a workspace that uses this checkout as the `go` SDK,
  `dagger module init go --dang-entrypoint` creates a module that
  `dagger -m <path> call` loads through the entrypoint. Constructor arguments
  and state, strings, lists, optional values, void, errors, nested Dagger
  calls, and core objects as results and arguments work. Enums fail as
  described above.
- `dagger generate` regenerates an existing entrypoint module, and it refuses
  the module when the scope setting is false.

Local commands:

```console
(cd helpers/codegen && go test ./...)
(cd helpers/module-codegen && go test ./...)
(cd helpers/render-template && go test ./...)
(cd helpers/entrypoint-contract && go test ./...)
dagger check
```

`dagger check` runs `engine-e2e`. It builds the pinned engine, runs the e2e
module inside it, and initializes and calls a module with `--dang-entrypoint`.
