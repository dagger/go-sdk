# naming

A copy of the Dagger engine's identifier naming rules,
[`engine/naming`](https://github.com/dagger/dagger/tree/main/engine/naming),
which Go codegen uses to name identifiers like the engine does for schemas
that have `Query.formatIdentifiers` (engine API views from v1.0.0-beta.21).

The generators run without an engine connection, and a module's own types
are only known after its Go package has been analyzed, so they can't ask the
engine's `Query.formatIdentifiers`. The copy must stay identical to the
engine's.

- Source: github.com/dagger/dagger@3bae26f41fbfd1ceb7f65ea515da3f72939e76a2
  (`VendoredCommit`), on the `naming/4-module-naming` branch
  (dagger/dagger#14669). Re-pin it to the commit that merges.
- Copied verbatim: `dictionary.go`, `format.go`, `naming.go`, `version.go` and
  `testdata/vectors.json`. `vendored.go` and `vendored_test.go` are go-sdk's.
- `KnownVersion` is the newest engine version whose rules the copy has.
  Codegen refuses a schema version past it (`CheckKnownVersion`) rather than
  apply rules that may be stale.

## Updating

1. Copy the files above from `engine/naming` at the new dagger commit.
2. Set `VendoredCommit` to that commit, and `KnownVersion` to the engine
   release it ships in (check `releases` in `version.go` for new
   dictionaries).
3. Run `go test ./internal/naming/` in `cmd/dagger-go-sdk-codegen`:
   `TestEngineVectors` checks the copy against the engine's vectors.
