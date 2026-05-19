# Helper Cleanup Design

This document records the helper boundary for the Go SDK Dang module after the
cleanup. Helpers are narrow wrappers around core API gaps. They are not where Go
SDK policy lives.

## Goals

- Prefer native Dang/core calls whenever they work.
- Use helpers only for module-context bugs or Dang ID rehydration bugs.
- Keep generation and workspace changeset rooting behind
  `github.com/dagger/sdk-sdk/polyfill`.
- Do not mount the caller workspace into helpers as `/ws`.
- Do not fake `.git` in the caller workspace. The update workaround may create
  a tiny synthetic `.git/HEAD` marker inside `/mock` only to give core a local
  context boundary.
- Do not expose raw GraphQL query helpers from the public Go SDK API.

## Bugs Being Worked Around

### Dang Cannot Rehydrate `*ID` Scalars

Dang currently treats GraphQL `*ID` scalar inputs as object handles. For example,
these fail during type inference:

```dang
loadModuleSourceFromID(id: stdout)
loadModuleSourceFromID(id: stdout :: ModuleSourceID!)
```

The explicit type hint changes the value type, but Dang still expects an object
handle for the `id` argument. This is tracked as `vito/dang#47`.

Raw GraphQL does not have that confusion:

```graphql
loadModuleSourceFromID(id: ModuleSourceID!): ModuleSource!
```

The remaining Go SDK dependency-update workaround still uses a nested raw
GraphQL helper. Generation no longer passes ModuleSource IDs through Go SDK
Dang code; that path moved to `sdk-sdk/polyfill`.

### Module-Side Source Loading Loses Caller Context

One source-loading call is unreliable from inside this module:

- `moduleSource(...)` cannot reliably access caller workspace files.

Until that core/context bug is fixed, source construction for generation
happens inside `sdk-sdk/polyfill`.

### `withDependencies` Rejects Workspace-Backed Directory Sources

The polyfill source helper produces workspace-backed directory module sources.
Passing those to `ModuleSource.withDependencies` currently fails with:

```text
unhandled module source kind: DIR_SOURCE
```

The old helper mounted the workspace at `/ws` to force `LOCAL_SOURCE`. That
workaround is intentionally removed. If `DIR_SOURCE` is rejected, that is a core
API bug to fix, not a helper behavior to preserve.

Dependency update is the exception: core's `withUpdateDependencies` still needs
`LOCAL_SOURCE` so it can skip existing local dependencies and update remote
dependencies. The workaround builds a dagger.json-only mock workspace, overlays
the edited module config, adds a synthetic `.git/HEAD`, and runs the update
query against that. It does not copy source files.

### Dang JSON Edits Need Two Workarounds

Dang can cast a literal core `JSON` scalar to a string:

```dang
toString("{}" :: JSON!)
```

`JSONValue.contents` is trickier. Calling `toString` on that result gives a JSON
string literal. To pass the rendered JSON to file APIs, decode the string once:

```dang
let rendered = toString(value.contents(pretty: true))
json().withContents(rendered :: JSON!).asString
```

The other wrinkle is that `dagger.json` dependencies are a legacy union shape:

```json
[
  "../dep",
  {"name": "other", "source": "github.com/acme/other"}
]
```

The core `JSONValue` API can read fields and set fields:

1. `File.asJSON`
2. `JSONValue.field`
3. `JSONValue.withField`
4. `JSONValue.contents`

`JSONValue.asArray` returns a GraphQL object list, so Dang list methods do not
work on it directly. This fails:

```text
json().withContents("[...]" :: JSON!).asArray.map { value => ... }
```

The workaround from <https://github.com/vito/dang/issues/46> does apply: select
a field first, then use list methods. For `JSONValue`, selecting `contents`
gives each array item as raw JSON:

```dang
json()
  .withContents("[...]" :: JSON!)
  .asArray
  .{contents}
  .map { value => toString(value.contents) }
```

That is enough for dependency list/add/remove. Those edits now live in the
polyfill module and are built from raw JSON fragments. Dependency update also
merges the query response in Dang. Local dependencies are preserved from the
original `dagger.json`; only remote dependencies returned by core are rewritten.

## Helper Shape

### `helpers/dagger-query`

CLI:

```sh
DAGGER_QUERY_VARS_JSON=... dagger-query --query /query.graphql --out /response.json
```

Output: `/response.json` plus any files/directories exported by the query.

Behavior:

1. Open a nested Dagger client.
2. Read the checked-in GraphQL query file.
3. Load variables from `DAGGER_QUERY_VARS_JSON`, which is raw JSON text.
4. Execute the raw GraphQL request.
5. Write the full response object to `--out`.

The helper is generic. It moved to `github.com/dagger/sdk-sdk/polyfill` and is
currently used only by dependency update.

## Dang Wrapper

`PolyfillNestedDaggerQuery` is private polyfill plumbing. Callers build `varsJSON` with
Dang's `toJSON(...)` builtin. The wrapper does not use core `JSONValue` for
query variables.

```dang
type PolyfillNestedDaggerQuery {
  """Return the container state after executing the query."""
  let outputContainer: Container!
}
```

The wrapper deliberately does not return a `Changeset`. The remaining update
query writes `/response.json`, and `PolyfillModuleConfig` decides how to merge
that response into `dagger.json`.

## Query Files

The remaining checked-in query file is the dependency update operation:

```text
polyfill/queries/module-source/updated-dependencies.graphql
```

Update query:

```graphql
query UpdatedDependencies($source: String!, $updates: [String!]!) {
  moduleSource(refString: $source, disableFindUp: true, requireKind: LOCAL_SOURCE) {
    withUpdateDependencies(dependencies: $updates) {
      dependencies {
        moduleName
        kind
        asString
        pin
      }
    }
  }
}
```

The update query does not call `generatedContextDirectory`. It asks core to
resolve the updated dependency set, then `PolyfillModuleConfig` writes remote
dependency updates back into the original `dagger.json`.

## Current Flow

Generation:

1. Call `polyfill.workspace(ws).moduleSource(path).generate`.
2. Return the `Changeset` from the generated `PolyfillWorkspaceFork`.

Init:

1. Build the seeded directory in Dang.
2. Return the staged template files and `dagger.json`.

Init intentionally does not generate. Generation is handled only for existing
workspace modules, which avoids converting a not-yet-written directory into a
module source.

Dependency add:

1. Edit `dagger.json` through `polyfill.workspace(ws).moduleSource(path).config`.
2. Return the config fork's changeset.

Dependency remove:

1. Edit `dagger.json` through `polyfill.workspace(ws).moduleSource(path).config`.
2. Return the config fork's changeset.

Dependency update:

1. Build a dagger.json-only mock workspace.
2. Include workspace `dagger.json` files so core can resolve local dependencies.
3. Overlay the edited module config and add `/mock/.git/HEAD` so core treats
   `/mock` as the local context root.
4. Run the update query against `/mock/<module>` as a `LOCAL_SOURCE`.
5. Merge returned remote dependency metadata into the original `dagger.json`;
   preserve local dependency entries exactly as written.
6. Return a changeset containing only the edited `dagger.json`.

Update-all skips local dependencies, matching core behavior. Updating a named
local dependency still fails with core's "updating local dependencies is not
supported" error.

Dependency edits deliberately do not run codegen. Users call `generate` when
they want generated SDK files refreshed.

## Deleted

The old `helpers/workspace-module-source` helper is removed. It contained the
operation-specific commands and the `/ws` workspace mount workaround.

The public `workspaceModuleSource` and `workspaceModuleSourceInclude` wrappers
are removed too. The former could not be implemented cleanly until Dang can
rehydrate helper-produced `ModuleSourceID` values. The latter was debug-only
introspection that exposed implementation details on the root API.

The local workspace fork shim, module-source helper, generated-context query,
module config editor, dependency-update query, and dagger-query helper moved to
`github.com/dagger/sdk-sdk/polyfill`.

## Removal Path

When core dependency mutation APIs are fixed, delete `helpers/dagger-query` and
the update query file from `github.com/dagger/sdk-sdk/polyfill`.

If core dependency mutation APIs are fixed first, prefer those APIs instead of
the mock-workspace update workaround.

## Verification

Helper builds:

```sh
cd helpers/render-template && go test ./...
```

Function surface:

```sh
dagger functions --progress=plain
```

Init generation smoke, from a git-initialized empty repo:

```sh
dagger -m /path/to/go-sdk call init --name foo added-paths
dagger -m /path/to/go-sdk call init --name foo --path ./foo/bar added-paths
```

Local dependency add/remove smoke:

```sh
dagger -m /path/to/go-sdk call mod --path app deps add --source ../lib --name lib modified-paths
dagger -m /path/to/go-sdk call mod --path app deps remove --name lib modified-paths
```

Dependency update smoke:

```sh
dagger -m /path/to/go-sdk call mod --path app deps update modified-paths
```

Named local dependency update should still fail with core's unsupported-local
message:

```sh
dagger -m /path/to/go-sdk call mod --path app deps update --name lib modified-paths
```
