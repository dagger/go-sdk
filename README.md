# Dagger Go SDK

Manage Dagger modules and module clients that use the Go SDK.
See [`docs/`](docs/) for module layouts, files outside the module directory,
and the release that generated code targets.

## The generated dang.toml

`dagger generate` writes `internal/dagger/entrypoint/dang.toml` beside each
generated Dang entrypoint:

```toml
dang = "2.1"

[imports.Dagger]
dagger = "v1.0.0-beta.21"
```

The `dagger` value is the release the module targets: the SDK's own release,
or the `targetVersion` setting.
It equals the `DAGGER_CLIENT_VERSION` value in the generated `main.dang`.
Commit this file with the entrypoint, and do not ignore it in `.gitignore`.
The engine loads a local module without its ignored files, so an engine that
reads this file would not see an ignored one.

### What generation changes in an existing file

The SDK manages the `dang` and `imports.Dagger.dagger` values of this file.
Generation replaces `dagger = true` or another version with the target.
Generation keeps other imports and other settings, such as
`imports.Dagger.service`.
Generation parses and rewrites the file, so comments and formatting are lost.
An unchanged file is not written again.

### What generation refuses

Generation stops and names the setting when this file sets one of these:

- a `dang` version other than `2.1`;
- a `path` for the `Dagger` import;
- a nonempty `endpoint` for the `Dagger` import;
- another import with a Dagger version other than the target.

### dang.toml files above the entrypoint

Generation never changes a `dang.toml` above the entrypoint.
Files do not merge, and the engine reads the nearest file in the module
directory.
A new entrypoint file would therefore hide a file in `internal/dagger`,
`internal` or the module directory.
Generation refuses when the nearest of those files sets anything beyond
`dang = "2.1"` and a `Dagger` import of `true` or the target.
A file above the module directory plays no part.

### Custom entrypoints

Generation does not touch the `dang.toml` of a custom runtime or a local
entrypoint that the SDK did not generate.
A generated entrypoint that replaces a remote one follows the rules above.

### Engines and tools

Released engines from v1.0.0-beta.18 to v1.0.0-beta.22 copy only `.dang` files
to evaluate an entrypoint, so they ignore this file.
[dagger/dagger#14902](https://github.com/dagger/dagger/pull/14902), which is not
merged, makes the engine read it to select the module's API view and naming
rules.
On such an engine, an entrypoint without a Dagger version gets
v1.0.0-beta.20, so run `dagger generate` to add the file to an existing module.

The `dang` value is the language version of the entrypoint source.
Parsing the versioned import in this file needs the Dang implementation v2.2.0
or later, so older standalone Dang tools can reject the file.
