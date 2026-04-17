package md

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Init(root))

	for _, dir := range Dirs {
		info, err := os.Stat(filepath.Join(root, dir))
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "%s should exist as dir", dir)

		info, err = os.Stat(filepath.Join(root, ArchiveDir, dir))
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "archive/%s should exist as dir", dir)
	}

	info, err := os.Stat(filepath.Join(root, RuntimeDir))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(gitignore), RuntimeDir)
}

func TestInitIdempotent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Init(root))
	require.NoError(t, Init(root))
}

func TestInitPreservesExistingGitignore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(root, 0o755))
	custom := "my-custom-rules\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte(custom), 0o644))

	require.NoError(t, Init(root))

	got, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, custom, string(got))
}

func TestEntityPath(t *testing.T) {
	root := t.TempDir()
	p, err := EntityPath(root, KindPerson, "jane-smith")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "people", "jane-smith.md"), p)
}

func TestEntityPathUnknownKind(t *testing.T) {
	_, err := EntityPath(t.TempDir(), "alien", "whatever")
	assert.Error(t, err)
}
