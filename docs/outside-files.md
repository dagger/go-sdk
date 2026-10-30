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

Every Dang entrypoint that go-sdk generates needs Dagger v1.0.0-beta.22 or
later, with or without files outside the module directory: the first release
whose Go library spells its identifiers as the engine names them, which the
generated code follows.
On v1.0.0-beta.17 and older, loading the module fails with
`field "types": missing argument "workspace" required by interface`.
The error prints the line above the entrypoint type, which reads
`# Needs Dagger v1.0.0-beta.22 or later; engines before v1.0.0-beta.18 report a missing "workspace" argument.`
Generation refuses an engine before v1.0.0-beta.22: it asks the engine for
the module's schema names (`ModuleSource.naming`) and its Go identifiers
(`formatIdentifiers`).

## The release the generated code targets

Each go-sdk release targets one Dagger release, v1.0.0-beta.22 today.
A module with the Dang entrypoint and unified clients pins
`dagger.io/dagger` in `go.mod` to exactly that release.
Its generated entrypoint declares the release to the engine, in the
`DAGGER_CLIENT_VERSION` variable of the process that runs the module; a
generated standalone client with a target announces it when it starts a
session.
The engine ignores `DAGGER_CLIENT_VERSION` today and serves the module at
its own version; a later engine release reads it and serves the declared
release instead.
The fixed core library and the module's own bindings depend on the SDK
release, the target setting and the module's sources.
An engine update leaves them unchanged.
Dependency clients still read the running engine's schema.
An engine update can change a dependency client if the engine's view of
that dependency changes.
An SDK update changes the targeted release unless `targetVersion` pins it.
The `targetVersion` setting pins an older release, between v1.0.0-beta.22
and the SDK's release; generation refuses a setting outside that range.
An engine older than the targeted release is not refused: a call to a
function that engine lacks fails when the module runs.
`targetVersion` replaces the `clientVersion` setting; generation refuses a
scope that still sets `clientVersion`.

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
Generation refuses a module's unified clients whose `dagger.io/dagger`,
required or replaced, is a release before v1.0.0-beta.22: that library spells
its identifiers by older rules, such as `SshfsVolume` for `SSHFSVolume`.

A module with embedded bindings or on the Go runtime targets the running
engine instead: an engine update can change its generated files, and the
`targetVersion` setting is refused there.
Stable targeting needs the Dang entrypoint with unified clients; see
Migration.

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

A module that uses the Go runtime keeps it after an SDK update.
With `dangEntrypoint` unset, generation keeps existing runtimes and local
entrypoints.
A module whose manifest names a remote `[entrypoint]` source, a module or
git reference, gets the generated entrypoint at its next `dagger generate`,
which reports the replacement.
This replacement also applies when `dangEntrypoint = false`.
The module then needs Dagger v1.0.0-beta.22 or later.
Before generating again, remove `dangEntrypoint = false` or set it to `true`;
the Go runtime cannot load a module with a generated entrypoint.
A local `[entrypoint]` path and a custom `[runtime]` stay the author's.
To switch one module, set these settings on its scope in `dagger.toml`, then
run `dagger generate`:

```toml
[sdks.go.scopes."path/to/module".settings]
dangEntrypoint = true
unifiedClients = true
globalClient = true
```

`unifiedClients = true` moves the bindings to the shared core library, so
the module targets one release instead of the running engine.
`globalClient = true` keeps the global `dag` that the module's code may use;
without it, generation fails with `undefined: dag`.
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

Generation names every Go identifier as the engine formats it, whatever the
engine's release or the schema's version.
Bindings generated before from a schema older than v1.0.0-beta.22 used
golint-style initialisms, so the next `dagger generate` renames a few core
identifiers, such as `ShortSha` to `ShortSHA`, `SshfsVolume` to `SSHFSVolume`
and `Sdks` to `SDKs`: code that uses the old names needs the new ones.
