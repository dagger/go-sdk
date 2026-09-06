# New default Go init template

> **Status: implemented.** See `templates/default/`, the `generateScope` wiring
> in `go-sdk.dang`, and the `e-2-e:module-init-check` test.

This is the Go counterpart of the TypeScript SDK's
[new default template](https://github.com/dagger/typescript-sdk/pull/10); the
two starters are kept deliberately parallel.

## Summary

When you run `dagger module init go <name>`, the SDK creates a new module from a
template. Today that template is an empty struct, so every user starts by
writing the same boilerplate: take the workspace source and build a container
from it.

This replaces the empty default with a small working module that already does
that. The empty struct is kept as the `empty` template, so `--template empty`
still gives a blank start.

## Plan

1. Add `templates/default` (below) and make it the template `init` uses by
   default.
2. Add `templates/empty`, the bare struct that used to be the default. It stays
   available as `--template empty`.
3. Keep `templates/legacy`, now reachable as `--template legacy` rather than
   through a dedicated `legacyTemplate` boolean.

## The template

```go
package main

import (
	"{{ .ModuleImport }}/internal/dagger"
)

type {{ .ModuleType }} struct {
	// Image the build runs on.
	BaseImageAddress string

	// +private
	Source *dagger.Directory
}

func New(
	ws *dagger.Workspace,
	// +default="alpine:3.21"
	baseImageAddress string,
) *{{ .ModuleType }} {
	return &{{ .ModuleType }}{
		BaseImageAddress: baseImageAddress,
		Source: ws.Directory("/", dagger.WorkspaceDirectoryOpts{
			Exclude: []string{"**/.git", "**/.dagger", "**/vendor"},
		}),
	}
}

// A container with the source mounted, ready to build on.
func (m *{{ .ModuleType }}) Container() *dagger.Container {
	return dag.Container().
		From(m.BaseImageAddress).
		WithDirectory("/src", m.Source).
		WithWorkdir("/src")
}
```

## What each function does

- **`New`** takes the `Workspace` (as `ws`) and the base image to build on. It
  loads the workspace root with `ws.Directory("/", dagger.WorkspaceDirectoryOpts{Exclude: ...})`,
  leaving out directories that shouldn't go into the build. The parameter is
  named `ws`, not `workspace`, because a `workspace` constructor arg generates a
  `--workspace` flag that collides with the top-level `--workspace` flag and
  makes the module fail to run.
- **`Container()`** returns a container with the source mounted. This is where
  the user adds their own build steps with `.WithExec([...])`.

## Design decisions

**The source comes from the workspace.** `New` takes a `Workspace` and calls
`ws.Directory("/", ...)` to load the workspace root. `Exclude` skips `.git`,
`.dagger`, and `vendor`; `Include` is available too if a module only cares about
part of the tree.

**`Source` is `+private`, `BaseImageAddress` is not.** Go module state has to
live in exported fields to survive between calls, and exported fields are part
of the module's API by default. `BaseImageAddress` is worth exposing — it is the
one knob the template offers. `Source` is an implementation detail, so `+private`
keeps it in state without putting it in the schema. This matches the TypeScript
starter, where `baseImageAddress` is `@func()` and `source` is not.

**Base image is `alpine:3.21`.** It is neutral and pinned to a version rather
than `latest`. The default cannot assume the user's project is Go — someone may
write a Go module that builds a Python repo or some infrastructure — so it does
not default to a `golang` image. We can pin by digest
(`alpine:3.21@sha256:...`) if we want stricter reproducibility.

**The template stays minimal.** It gives the user a source directory and a
container to build on, and nothing else. The goal is to save time, not to teach:
there are no publish, check, or test functions to read through and delete. That
is also what separates it from `legacy`, the pre-1.0 scaffold with its
`ContainerEcho`/`GrepDir` demo functions, which stays available for anyone who
wants the old output.

## Implementation notes

[`go-sdk.dang`](../go-sdk.dang) selects the configured template and rejects an
unknown name. All templates use the same `renderedTemplate` helper.

`render-template` substitutes `{{ .ModuleName }}` (raw), `{{ .ModuleType }}`
(camel-cased, the struct name), and `{{ .ModuleImport }}` (`dagger/<kebab-name>`,
the generated module's import path). The templates use the latter two, so no new
tokens are needed.
