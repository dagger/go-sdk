package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOwnedRejectsUntrackedUserFiles(t *testing.T) {
	root, generated := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "dagger.gen.go"), []byte("package user\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(generated, "dagger.gen.go"), []byte("package generated\n"), 0644))
	err := runValidateOwned([]string{"--root", root, "--generated", generated})
	require.ErrorContains(t, err, "contains user changes; refusing to overwrite it")
	data, err := os.ReadFile(filepath.Join(root, "dagger.gen.go"))
	require.NoError(t, err)
	require.Equal(t, "package user\n", string(data))
}

func TestValidateOwnedAcceptsNewFilesWithoutWriting(t *testing.T) {
	root, generated := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(generated, "dagger.gen.go"), []byte("package generated\n"), 0644))
	require.NoError(t, runValidateOwned([]string{"--root", root, "--generated", generated}))
	_, err := os.Stat(filepath.Join(root, "dagger.gen.go"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
