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
	sch := fixtureSchema(t)
	require.NoError(t, Init(root, sch))

	for _, entity := range sch.Entities {
		info, err := os.Stat(filepath.Join(root, entity.Dir))
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "%s should exist as dir", entity.Dir)

		info, err = os.Stat(filepath.Join(root, sch.Store.ArchiveDir, entity.Dir))
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "archive/%s should exist as dir", entity.Dir)
	}

	info, err := os.Stat(filepath.Join(root, sch.Store.RuntimeDir))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(gitignore), sch.Store.RuntimeDir)
}

func TestInitIdempotent(t *testing.T) {
	root := t.TempDir()
	sch := fixtureSchema(t)
	require.NoError(t, Init(root, sch))
	require.NoError(t, Init(root, sch))
}

func TestInitPreservesExistingGitignore(t *testing.T) {
	root := t.TempDir()
	sch := fixtureSchema(t)
	require.NoError(t, os.MkdirAll(root, 0o755))
	custom := "my-custom-rules\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte(custom), 0o644))

	require.NoError(t, Init(root, sch))

	got, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, custom, string(got))
}

func TestEntityPathSprawl(t *testing.T) {
	s := newTestStore(t)
	p, err := s.EntityPath("person", "jane-smith")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(s.Root(), "people", "jane-smith", "index.md"), p)

	folder, sprawl, err := s.EntityFolder("person", "jane-smith")
	require.NoError(t, err)
	assert.True(t, sprawl)
	assert.Equal(t, filepath.Join(s.Root(), "people", "jane-smith"), folder)
}

// TestEntityPathFlat pins the `flat: true` escape hatch: entities that
// opt out of sprawl keep the legacy single-file layout.
func TestEntityPathFlat(t *testing.T) {
	s := newFlatTestStore(t)
	p, err := s.EntityPath("tag", "vip")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(s.Root(), "tags", "vip.md"), p)

	_, sprawl, err := s.EntityFolder("tag", "vip")
	require.NoError(t, err)
	assert.False(t, sprawl)
}

func TestEntityPathUnknownKind(t *testing.T) {
	s := newTestStore(t)
	_, err := s.EntityPath("alien", "whatever")
	assert.Error(t, err)
}
