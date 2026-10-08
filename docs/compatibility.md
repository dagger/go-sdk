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

The legacy-target probes do not prove that a beta.16 module can be regenerated
or migrated automatically. Validate those paths separately on a frozen
beta.16 fixture. The beta.16 floor check currently covers standalone clients,
not the complete entrypoint/unified mode.

Existing modules keep the Go runtime unless `dangEntrypoint` is enabled.
Modules that share a parent go.mod or sibling Go source keep that runtime.

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

`unifiedClients` opts into shared `dagger.io/dagger/core` types. Generated
module clients use `lib.New()` for the lazy process session or `lib.New(dag)`
to borrow a `dagger.Connect` connection. Closing an explicit connection leaves
its bindings unusable; they never silently reconnect. Reopening the default
session creates a new module-loading cache. Bindings include the target address
and immutable pin; successful loads are cached per connection. Failed loads can
be retried. A missing schema field reports that bindings need regeneration;
resolver and transport errors retain their original meaning.

`clientVersion` selects the runtime SDK independently of each target's schema
version. Unified clients require an SDK release containing the core split and
`ModuleGraphQLClient` and core handle argument codecs, or a local
`dagger.io/dagger` replacement during development. A target's older schema
version must not select an older SDK for the new caller.

To check an old target with the local runtime:

```sh
dagger -m .dagger/modules/e2e call unified \
  --runtime-sdk /path/to/dagger/sdk/go --legacy-version v0.21.7 \
  calls-check --ws . pass
```

Repeat with `v0.20.8` and `v0.18.0`. The same check set also includes
`git-calls-check`, `changed-target-check`, and `stale-target-check`. Add
`--package-main` to `unified` to exercise a caller that keeps `package main`.

Generation records file hashes in `.dagger-generated.json`. Removing a client
removes unchanged owned outputs and preserves user files and edited generated
files. A collision with user changes fails before generated files are written.
Old outputs with a Dagger generated header can be adopted on the first run.

Before changing the default for new modules or cutting a tag:

1. Integrate the shared harness, smoke assertion, floor, and regression fixes
   from go-sdk #51–#56, retaining one canonical collection test.
2. Merge the core split (#14186, including #14234 and the already-integrated
   #14559), then the runtime/session and enum fixes (#14561/#14562). Include
   the complementary collection and file-preservation fixes (#14447/#14572).
3. Review the complete preserved entrypoint foundation in #48 and the unified
   clients in #49. Resolve overlapping changes in #43/#25. Finish the
   no-global-client behavior and an explicit compatibility option before
   changing defaults; today's generated code still contains global clients.
4. Update #14560 to a generator revision containing #49's core codec emitter,
   regenerate against the integrated schema, and integrate it into #14240.
   Then merge #14240's generator adoption into main.
5. Record immutable published generator and runtime SDK pins. Run the SDK
   checks on the integrated engine, including unchanged and regenerated
   beta.16 modules, explicit migration, old-target clients, Git and directory
   calls, cache invalidation, enums, and collections.
6. Record the new mode's released engine floor once its prerequisites ship.
   Enable entrypoints for new modules after these gates pass. Preserve existing
   modules' runtime choice; switching an existing module remains explicit.
7. Tag the SDK after review. Remote publication requires maintainer approval.
