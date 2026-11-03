# go-sdk

> [!NOTE]
>
> By default, this version of the Go SDK targets Dagger engine
> `v1.0.0-beta.24`. See
> [The release the generated code targets](docs/outside-files.md#the-release-the-generated-code-targets).

A Dagger module for managing Dagger modules and module clients that use the
Go SDK.
See [`docs/`](docs/) for module layouts, files outside the module directory,
and the release that generated code targets.

## The generated dang.toml

`dagger generate` writes `internal/dagger/entrypoint/dang.toml` beside each
generated Dang entrypoint:

```toml
dang = "2.1"

[imports.Dagger]
dagger = "v1.0.0-beta.24"
```

The `dagger` value is the release the module targets: the SDK's own release,
v1.0.0-beta.24, or the `targetVersion` setting.
It equals the `DAGGER_CLIENT_VERSION` value in the generated `main.dang`.
The `dang` value is the Dang language version of the entrypoint source,
which stays `2.1`.
Commit this file with the entrypoint, and do not ignore it in `.gitignore`.
The engine loads a local module without its ignored files, so an engine that
reads this file would not see an ignored one.

### When generation writes the file

Generation writes the file for unified and embedded bindings alike, when it:

- generates a new module with the Dang entrypoint;
- migrates a Go runtime module to the Dang entrypoint, which needs
  `dangEntrypoint = true`;
- replaces a remote entrypoint with the generated one;
- generates an entrypoint again.

Generating again adds the file to an entrypoint that an older SDK generated
without it, and follows a change of the target.
A module that keeps the Go runtime gets no file, and neither does a scope
without a module.
An existing Go runtime module keeps the Go runtime while `dangEntrypoint` is
unset.

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
Files do not merge: an engine that reads `dang.toml` uses the nearest file
between the entrypoint and the module directory.
A new entrypoint file would therefore hide a file in `internal/dagger`,
`internal` or the module directory.
Generation refuses when the nearest of those files sets anything beyond
`dang = "2.1"` and a `Dagger` import of `true` or the target.
A file above the module directory plays no part.

### Custom entrypoints

With `dangEntrypoint` unset or `false`, generation keeps a custom runtime or
a local entrypoint that the SDK did not generate, and its `dang.toml`,
unchanged.
Generation refuses such a module with `dangEntrypoint = true`, and changes no
file.
A generated entrypoint that replaces a remote one follows the rules above,
also with `dangEntrypoint = false`; generation does not change the remote
entrypoint's own `dang.toml`.

### Engines and tools

Released engines from v1.0.0-beta.18 to v1.0.0-beta.24 copy only `.dang`
files to evaluate an entrypoint, so they ignore this file and serve the
module at their own version.
[dagger/dagger#14902](https://github.com/dagger/dagger/pull/14902) and
[dagger/dagger#14958](https://github.com/dagger/dagger/pull/14958), which are
not merged, make the engine read the `Dagger` version of this file and serve
the module at that release.
With those changes, an entrypoint without a Dagger version gets
v1.0.0-beta.24, and the identifier-casing cut is in v1.0.0-beta.25.
The default target of this SDK equals that fallback, but a module that pins
an older release needs the file.
Run `dagger generate` to add the file to a module whose entrypoint an older
SDK generated.

Parsing the versioned import in this file needs the Dang implementation
v2.2.0 or later, so older standalone Dang tools can reject the file.
