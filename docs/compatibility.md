# Go SDK compatibility

Pre-1.0 Go modules must continue to work on the new engine. This includes
v0.21.x modules and earlier releases. Their existing `dagger.json`, generated
bindings, `dagger.io/dagger` imports, and pinned SDK versions remain usable;
they do not need to adopt `dagger.io/dagger/core` to be called by a new module.

The compatibility checks exercise v0.21.7, v0.20.8, and v0.18.0 targets. Each
target uses the public SDK API from before the core split and exposes container
inputs and outputs. The new caller uses shared core types across that boundary.
Run these checks on the integrated new engine. Compatibility with an older
beta engine is a separate concern.

Existing beta.16 modules that keep their generated code, manifest, and SDK pin
must also remain loadable. This is different from upgrading the caller's Go
SDK dependency: the core split moves public types to `dagger.io/dagger/core`
and removes generated API methods from `dagger.Client`. That source upgrade
requires migration; for example, `client.Container()` becomes
`core.NewQuery(client).Container()`.

The legacy-target probes and the beta.16 standalone-client floor check do not
prove entrypoint migration. The separate beta.16 migration check first generates
and calls a fixture on released beta.16, then migrates it on the integrated
engine. Before migration, it also calls the frozen module on the new engine
and verifies that its source, SDK pin, manifest, and bindings stay unchanged.
Migration verifies unchanged author source, a retained global `dag`, stable
regeneration, and actual module calls passing core objects in both directions.
This does not establish beta.16 as the engine floor for the complete new mode.

New module scopes use a Dang entrypoint and shared core types by default.
They do not generate the unqualified global `dag`. Existing modules keep their
Go runtime or custom entrypoint unless explicitly migrated. Existing generated
Dang entrypoints keep that mode. Modules using a parent go.mod keep their
existing runtime; fresh nested scopes receive their own go.mod.

`dangEntrypoint` and `unifiedClients` can be set explicitly to override automatic
selection. Existing entrypoint and standalone clients keep their previous mode.
Standalone clients outside an entrypoint module keep embedded bindings unless
`unifiedClients = true` is explicit. This avoids upgrading an existing Go
application's runtime SDK merely because a client was added. Use the generated `dagger.Connect()`
accessor or, with unified clients, `core.NewContainer()` and the other core
constructors. These use the
shared default session; removing the API global does not open a connection
for each call.

`globalClient` controls compatibility for a module's author package:

| Setting | Generated behavior |
| --- | --- |
| Unset | Preserve the saved choice; otherwise enable compatibility for a recognized legacy generated client or Go runtime manifest. Fresh modules omit `dag`. |
| `true` | Emit `var dag = dagger.Connect()` for existing author code. |
| `false` | Omit `dag`, even if legacy files are present. Author code must already use explicit accessors or constructors. |

For example, an existing module can opt into the new generator while retaining
its author API in the workspace configuration:

```toml
[sdks.go.scopes.".dagger/modules/example".settings]
dangEntrypoint = true
globalClient = true
```

The generator records the effective choice in SDK-owned
`.dagger-generated.json`; it does not edit the engine-owned `dagger.toml`.
Explicit `false` takes precedence over automatic migration, and a failed
generation leaves author files unchanged. A later automatic generation keeps
the saved decision.

This follows the [Python SDK's migration policy](https://github.com/dagger/python-sdk/blob/8506bf5c7991aaca501713df73af7da70cff9468/README.md#migrate-a-module):
new projects omit the author global, recognized legacy projects
retain it, and an explicit opt-out takes precedence. Python saves
`global-client` under `[tool.dagger]` in `pyproject.toml`. Go saves the inferred
choice in `.dagger-generated.json`, while explicit settings stay in the
workspace configuration. Both continue to share connections when the author
global is removed.

Legacy runtime generation retains its global API. It rejects an explicit
`globalClient = false` until the project enables Dang entrypoints. The
compatibility option does not recreate the old combined dependency API
(`dag.Dependency()`): projects using that API keep the legacy runtime until
their source migrates to standalone clients. Entry-point migration continues
to reject dependency manifests that it cannot preserve.

Dang entrypoints support package main and importable packages. The module
directory must contain its own go.mod. Generation builds from the module's
own source and leaves author files unchanged. Enum names and collection
markers come from the same analyzer as the Go runtime.

Manifest v2 first shipped in beta.14; collections and reliable explicit git
pins shipped in beta.15. Beta.15 alone is not the release floor for the complete
new mode. Cache policies need the engine's later call-digest support, unified
clients need the later serveModule correctness fixes, and owning-module enums
need the accompanying enum lookup fix. Validate the integrated engine stack
before claiming a released floor for these features.

`unifiedClients = true` selects shared `dagger.io/dagger/core` types explicitly.
Generated module clients use `lib.New()` for the lazy process session or `lib.New(dag)`
to borrow a `dagger.Connect` connection. Closing an explicit connection leaves
its bindings unusable; they never silently reconnect. Reopening the default
session creates a new module-loading cache. Bindings include the target address
and immutable pin; successful loads are cached per connection. Failed loads can
be retried. A missing schema field reports that bindings need regeneration;
resolver and transport errors retain their original meaning.

`clientVersion` selects the runtime SDK independently of each target's schema
version. Automatic selection uses the published
`v1.0.0-beta.16.0.20261008202843-134540fec551` runtime for unified clients.
That runtime contains the core split, shared module sessions, and core object
ID decoding. Legacy clients retain their target-version SDK unless an explicit
`clientVersion` overrides it. A target's older schema version must not select
an older SDK for a new unified caller.

Local replacements used by entrypoint generation must be reachable within the
module source directory. A sibling replacement outside that directory is not
included in the staged build.

To check an old target with the local runtime:

```sh
dagger -m .dagger/modules/e2e call unified \
  --runtime-sdk /path/to/dagger/sdk/go --legacy-version v0.21.7 \
  calls-check --ws . pass
```

Repeat with `v0.20.8` and `v0.18.0`. The same check set also includes
`git-calls-check`, `changed-target-check`, and `stale-target-check`. Add
`--package-main` to `unified` to exercise a caller that keeps `package main`.

To exercise the complete proposed engine stack and this SDK checkout:

```sh
dagger -m .dagger/modules/engine-e2e call integrated-sdk-check \
  --ws . --engine-source /path/to/dagger pass
dagger -m .dagger/modules/engine-e2e call beta-16-migration-check \
  --ws . --engine-source /path/to/dagger pass
```

The first check includes global removal, explicit compatibility settings,
current and old-target calls, Git, changed targets, removed clients, and package
main. The second generates and calls a real fixture on released beta.16, then
migrates its generated module API on the supplied engine without changing
author source. It also calls the original frozen module on that engine before
migration and checks that loading preserves its source, pin, and bindings.
These commands must return `true`; a check's `pass` field can return `false`
without a failing process exit code.

Generation records file hashes in `.dagger-generated.json`. Removing a client
removes unchanged owned outputs and preserves user files and edited generated
files. A collision with user changes fails before generated files are written.
Old outputs with a Dagger generated header can be adopted on the first run.

Remaining integration order:

1. The engine prerequisites (#14186, #14561, #14562, #14447, and #14572)
   are merged. The core split includes Tom's #14234 and #14559.
2. Go SDK #48 and Tibor's #57 are merged, along with Yves's #51–#56.
   Review and merge #49 next: it adds shared clients, new-module defaults,
   and the global-client compatibility option on top of those fixes.
   #49 includes the older-schema fixes from #43; any remaining package-naming
   work from #25 must preserve existing import names.
3. Update Dagger #14560 to the published generator containing #49 and
   regenerate against the actual engine schema. Merge #14560 into
   `go-core-codegen` (#14240), then merge #14240 into Dagger main after CI.
4. Record the immutable generator and runtime pins and the released engine
   floor for the new defaults. Check unchanged beta.16 loading, explicit
   migration, old-target calls, Git, cache invalidation, enums, and collections
   on the integrated stack; a standalone-client floor check is insufficient.
5. Tag the SDK after review and the engine floor release.
