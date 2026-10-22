package main

import (
	"testing"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/stretchr/testify/require"
)

// A module of the Git workspace is bound the same way at every commit, so its
// generated client does not change from one commit to the next.
func TestWorkspaceClientIsIndependentOfCommit(t *testing.T) {
	bound := func(commit string) generator.BoundModule {
		module := generator.BoundModule{Kind: generator.ModuleKindGit, Path: ".dagger/modules/go", Ref: "https://github.com/o/r#" + commit + ":.dagger/modules/go", Pin: commit}
		meta := clientMeta{Module: module, Workspace: workspaceMeta{Address: "github.com/o/r@" + commit, Commit: commit}, Git: gitMeta{CloneRef: "https://github.com/o/r"}}
		got, inWorkspace := clientBoundModule(meta)
		require.True(t, inWorkspace)
		return got
	}
	first := bound("419c4afea78603265c40e273c455623a0291922e")
	require.Equal(t, generator.BoundModule{Kind: generator.ModuleKindLocal, Path: ".dagger/modules/go"}, first)
	require.Equal(t, first, bound("0000000000000000000000000000000000000001"))
}

// A module of another repository keeps its ref and pin.
func TestForeignClientKeepsItsRef(t *testing.T) {
	const sha = "419c4afea78603265c40e273c455623a0291922e"
	module := generator.BoundModule{Kind: generator.ModuleKindGit, Path: "app", Ref: "https://github.com/other/r#" + sha + ":app", Pin: sha}
	meta := clientMeta{Module: module, Workspace: workspaceMeta{Address: "github.com/o/r@" + sha, Commit: sha}, Git: gitMeta{CloneRef: "https://github.com/other/r"}}
	got, inWorkspace := clientBoundModule(meta)
	require.False(t, inWorkspace)
	require.Equal(t, module, got)
}
