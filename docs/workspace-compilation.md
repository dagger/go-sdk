# Workspace compilation and caching

A Go module can compile code from another directory in its checkout, for
example through a local `replace` in `go.mod`. That code can also embed files
with `go:embed`. The Go SDK discovers these inputs automatically; you do not
need to list them in `//go:resources`.

There are two caches to consider:

1. **Function results.** Dagger checks this cache before the SDK runs. Every
   exported constructor and function of a module with external compiler inputs
   must take a required `ws *dagger.Workspace`. Another module passes its
   workspace when calling them, for example `goClient.New(ws).Build(ws)`.
   The CLI supplies the workspace automatically for direct CLI calls.
   The workspace is part of the call's input, so a fresh local workspace session
   makes the SDK check current files again. The SDK enforces these arguments
   during generation: missing or optional arguments produce an error.
2. **Go compilation.** The SDK follows imports, local `replace` targets, and
   `go:embed`, then imports the selected external files. The build caches by
   file content. A changed compiler input triggers a new build. Unchanged
   inputs can reuse the previous build, even though the function runs again.

For example, both calls take the workspace:

```go
package app

import (
    "example.com/app/internal/dagger"
    "example.com/lib/greet"
)

type App struct{}

func New(ws *dagger.Workspace) *App { return &App{} }

func (*App) Greeting(ws *dagger.Workspace) string {
    return greet.Greeting()
}
```

The SDK uses `ws` to compile the module, even though `Greeting` never reads it
in its Go body. Giving only `New` a workspace would still allow Dagger to reuse
an old `Greeting` result.

If `greet` embeds `greeting.txt`, changing that file changes the compiled value
on the next fresh workspace call, without regenerating the module. Editing an
unrelated file that the compiler does not need can reuse the existing build.

Passing a workspace is lazy. Discovery reads Go source, module metadata and
file names; the SDK then imports the selected external contents. Module
authors still choose the files their own pipelines read. The Go SDK handles
compiler input selection.

This uses existing workspace behavior and needs no engine change or entrypoint
resource tracking. Self-contained modules do not need this extra workspace
requirement for compilation.
