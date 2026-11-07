# Files outside the module directory

A Go module can build code from another directory of its repository through
a local `replace` directive in `go.mod`.
That code can embed files with `//go:embed`.
Every Dang entrypoint that go-sdk generates declares a `sources` function.
`sources` returns the module directory and the files the build reads outside
it.
The rest of the entrypoint is the same for every module.

## What generation writes

`dagger generate` scans the module and finds the files its build reads
outside the module directory:

- the `go.mod` of each local replace target that `go.mod` requires;
- the Go files of each package the module imports from a replace target,
  and of the packages those import from a replace target;
- the files those packages embed, and their assembly, header and syso files.

The scan counts imports under every build tag and in the module's test files.
Generation writes the patterns of these files into `sources`.
For a module whose build reads nothing outside its directory, `sources`
returns the module directory only.

Generation also lists the same files in the module's `dagger-module.toml`, as
`include`, relative to the module directory. The list excludes every test
file outside the module and keeps the module's own, as `sources` does. A
module whose build reads nothing outside gets `include = []`.
An engine that reads a version 2 manifest's `include` loads those files with
the module and does not run `sources`, even for an empty list, so loading the
module evaluates no Dang code. A released engine ignores `include` and runs
`sources`. The module loads on both.

The `go mod tidy` of generation removes a requirement that nothing imports,
so `sources` does not return the replace target of that requirement.
When a dependency outside the workspace still needs packages of that replace
target, generation fails.
The generation error ends with one line per such replace target, for example
`warning: go.mod:7: replace example.com/lib => ../lib: the module imports no package of it, so only ../lib/go.mod reaches the build`.
A replace target that is neither required nor imported is unused.
Generation ignores an unused replace target.

For example, `app/go.mod` replaces `example.com/lib` with `../lib`, and
`app/main.go` imports `example.com/lib/greet`, which embeds `greeting.txt`:

```go
package app

import "example.com/lib/greet"

type App struct{}

func (*App) Greeting() string { return greet.Greeting() }
```

Generation writes this function into `app/internal/dagger/entrypoint/main.dang`:

```dang
pub sources(workspace: Workspace!): Directory! {
  directory
    .withDirectory(workspace.cwd + "/..", workspace.directory("..",
      include: ["lib/go.mod", "lib/greet/*.go", "lib/greet/*.h", "lib/greet/*.s", "lib/greet/*.syso", "lib/greet/greeting.txt"],
      exclude: ["**/.git", "**/*_test.go"],
      gitignore: true,
    ))
    .withDirectory(workspace.cwd, workspace.directory(".", exclude: ["**/.git"], gitignore: true))
}
```

## How the engine uses sources

The engine calls `sources` when it loads the module.
`sources` runs with a working client and the same trust as module code.
The workspace that `sources` receives is rooted at the module's context root,
and its current directory is the module directory.
`sources` returns the module directory and the outside files at their paths
from the workspace root.
The engine adds the returned files to the module context.
The engine keys the module's types and every function result on the content
of that context.

A function result is reused across sessions until a file that `sources`
returns changes.
A change to another file outside the module directory does not invalidate a
result.
Exported functions need no workspace argument for the build.

The entrypoint builds the module and runs each call with the context mounted
at `/workspace` and the module directory as the working directory.
A function can therefore read `../lib/greet/greeting.txt` when it runs.

## Engine versions

Every Dang entrypoint that go-sdk generates needs Dagger v1.0.0-beta.18 or
later, with or without files outside the module directory.
On v1.0.0-beta.17 and older, loading the module fails with
`field "types": missing argument "workspace" required by interface`.
The error prints the line above the entrypoint type, which reads
`# Needs Dagger v1.0.0-beta.18 or later; older engines report a missing "workspace" argument.`
Generation refuses on v1.0.0-beta.16 and older.

## The release the generated code targets

Each go-sdk release targets one Dagger release, v1.0.0-beta.25 today.
A module with the Dang entrypoint and unified clients pins
`dagger.io/dagger` in `go.mod` to exactly that release.
Its generated entrypoint declares the release to the engine, in the
`DAGGER_CLIENT_VERSION` variable of the process that runs the module; a
generated standalone client with a target announces it when it starts a
session.
Released engines up to v1.0.0-beta.21 ignore `DAGGER_CLIENT_VERSION` and
serve the module at their own version.
v1.0.0-beta.22 to v1.0.0-beta.25 read it, which moves the core API view of
the module's process; they still serve the module at their own version.
The generated `internal/dagger/entrypoint/dang.toml` declares the same
release as its `Dagger` import; see the
[README](../README.md#the-generated-dangtoml).
The fixed core library and the module's own bindings depend on the SDK
release, the target setting and the module's sources.
An engine update leaves them unchanged.
Dependency clients still read the running engine's schema.
An engine update can change a dependency client if the engine's view of
that dependency changes.
An SDK update changes the targeted release unless `targetVersion` pins it.
The `targetVersion` setting pins a release between v1.0.0-beta.18 and
v1.0.0-beta.26, the release in flight; generation refuses a setting outside
that range.
A release after the SDK's own generates on an engine of that release, and
with unified clients needs its `dagger.io/dagger` published or replaced.
An engine older than the targeted release is not refused: a call to a
function that engine lacks fails when the module runs.

When another requirement of `go.mod` needs a later `dagger.io/dagger`,
generation refuses and names that requirement: raise `targetVersion`, or
require a version that needs no later library.
A scope without a module pins the library of its clients the same way, to
`targetVersion` when set and to the SDK's release when a client uses shared
core types, and checks it after its own `go mod tidy`.
A client of another module aliases the core types of the targeted release.
A `replace` directive for the selected `dagger.io/dagger` is a development
exception: generation keeps it, reports it, and the generated code follows
the replacement instead of the targeted release.

A module with the Dang entrypoint and embedded bindings targets the same
release: its bindings are generated from that release's schema, and its
entrypoint declares that release.
`targetVersion` pins its release the same way.
Its bindings come from the running engine's view of that release, so an
engine update can still change them.

A module on the Go runtime refuses `targetVersion`.
A new manifest declares the SDK's release as its `engineVersion`; an
existing manifest keeps the `engineVersion` it declares, or gets the running
engine's when it declares none, and the engine generates the module's code
for that version.
Fully pinned targeting needs the Dang entrypoint with unified clients; see
Migration.

## Removed settings

go-sdk no longer has the `clientVersion` and `fat` settings.
Generation writes no `dagger.json`; it migrates an existing one into
`dagger-module.toml` and removes it.
The CLI refuses either setting as an argument, for example
`dagger module init go --fat` fails with `unknown flag: --fat`.
In `dagger.toml`, the engine ignores an unknown setting under
`[modules.go-sdk.settings]` or under a scope's settings, so a stale
`clientVersion` or `fat` there has no effect and no error.
A stale `clientVersion` no longer pins the target: replace it with
`targetVersion`, which keeps the pin.
Remove a stale `fat`.

```toml
[sdks.go.scopes."path/to/module".settings]
targetVersion = "v1.0.0-beta.20"
```

`dagger doctor` reports unknown settings under `[modules.<name>.settings]`;
check the settings of each scope by hand.

## When to generate again

Run `dagger generate` again after one of these changes:

- a change to the replace directives of `go.mod`;
- a new import of another package from a replace target;
- a new `//go:embed` directive in an imported package.

A new Go, assembly, header or syso file in an imported package needs no
generation.
A new file that matches an existing embed pattern needs no generation.
When the build misses a file, it fails with `The build misses files outside
the module directory: run dagger generate`.

## Limits

- A function cannot read another file outside the module directory: reading
  one fails with `open ../x: no such file or directory`.
- A replace target must stay inside the workspace. Generation refuses a
  target above the workspace root.
- A version-specific replace whose version only a dependency's `go.mod`
  selects can add extra files to `sources` when the replace targets of
  several versions exist.
- A version-specific replace of a nested module path that `go.mod` does not
  require, such as `example.com/a/sub v1.0.0`, hides the packages under that
  path from a required replace of its parent `example.com/a`: `sources`
  misses their files and the build fails.
- Generation refuses a symlink that leads outside the module directory.
- `sources` returns no file that `.gitignore` ignores. Generation refuses a
  pattern that matches only ignored files.

## Migration

When unset, `dangEntrypoint` and `unifiedClients` select the generated
entrypoint and unified bindings for a new module. On `dagger generate`,
existing modules keep their runtime or entrypoint and binding style.
Set both settings to `true` to migrate a Go runtime module to the generated
entrypoint with unified bindings.
The module then needs Dagger v1.0.0-beta.18 or later.
The generated entrypoint loads the module's types without running it.
It supports `package main` and importable packages, and needs a `go.mod` in
the module directory: generation refuses a module that shares the `go.mod`
of a parent directory.
In a scope without a module, an unset `unifiedClients` keeps existing
clients' binding styles and uses unified bindings for new clients.

| `dangEntrypoint` | `unifiedClients` | Generation |
|---|---|---|
| unset | unset | New module: generated entrypoint and unified bindings. Existing module: retain runner and bindings. |
| `true` | `true` | Migrate to the generated entrypoint with unified bindings. |
| `true` | unset | Generated entrypoint; retain existing bindings, or use unified bindings if none exist. |
| `false` | unset or `false` | Go runtime with its embedded bindings. |
| `true` | `false` | Generated entrypoint with embedded bindings; such a module cannot use other modules. |
| `false` | `true` | Refused: the Go runtime cannot generate unified bindings. A custom runner is exempt; see below. |

To explicitly keep the Go runtime, set both settings on the module's scope in
`dagger.toml`:

```toml
[sdks.go.scopes."path/to/module".settings]
dangEntrypoint = false
unifiedClients = false
```

A module with a generated entrypoint cannot return to the Go runtime.
A module whose manifest names a remote `[entrypoint]` source, a module or
git reference, gets the generated entrypoint at its next `dagger generate`,
which reports the replacement.
This replacement also applies when `dangEntrypoint = false`.

Generation does not replace a custom `[runtime]` or a local `[entrypoint]`
path that the SDK did not generate.
With `dangEntrypoint` unset or `false`, generation keeps the runner and its
`dang.toml`, and only generates the standalone clients that the scope's
`clients` list.
Explicit `dangEntrypoint = true` is refused without changing any files.
An unset `unifiedClients` keeps the style of each existing client package
and generates a new one with embedded bindings; `true` or `false` sets the
style of every client.

The migration removes the global `dag` unless `globalClient = true`.
When the module's code still uses `dag`, generation fails with
`undefined: dag` and names the setting, and changes no file:

```toml
[sdks.go.scopes."path/to/module".settings]
dangEntrypoint = true
unifiedClients = true
globalClient = true
```

Unified bindings take the module's core types from one fixed library
release instead of the running engine's schema.
The same settings under `[modules.go-sdk.settings]` apply to every Go module
of the workspace.
`dagger generate` skips a module that fails to load: it shows the error only
on the module's span, prints `no changes to apply` and exits 0.

PR #64 compiled such modules from a workspace argument instead.
go-sdk `3d4c37a0a2` merges PR #63, which contains the design of PR #64.
To migrate a module generated by that commit:

1. Remove the `ws *dagger.Workspace` arguments that served only compilation.
2. Remove the `ws` arguments from the calls of the module's functions in
   other modules, for example `appClient.New(ws).Greeting(ctx, ws)` becomes
   `appClient.New().Greeting(ctx)`.
3. Run `dagger generate`.

Generation removes the vendored `internal/dagger/entrypoint/go-inputs`
directory.
A function that keeps a `ws *dagger.Workspace` argument still works, but the
engine passes a new workspace to it on every call, so its results are never
reused.

Generation removes `include` from `dagger-module.toml` and `dagger.json`:
the local replace directives of `go.mod` select the files outside the module
directory.
The removal shows in the changes that `dagger generate` applies.
A file outside the module directory that only a function reads at run time is
no longer available.

Generation also removes `codegen`: only the engine's codegen for the Go
runtime reads `automaticGitignore`.
It removes `dependencies` when the scope's `clients` in `dagger.toml` list
every dependency by module name: the entrypoint reaches them through the
generated clients.
It refuses a dependency that the scope's `clients` do not list, and names it.
