package md

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveRoundtripSprawl(t *testing.T) {
	s := newTestStore(t)

	// Seed a sprawl entity: folder with index.md plus a sibling file that
	// should travel with the folder on archive.
	folder := filepath.Join(s.Root(), "people", "jane-smith")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	index := filepath.Join(folder, "index.md")
	sibling := filepath.Join(folder, "notes.md")
	require.NoError(t, os.WriteFile(index, []byte("---\nfirst_name: Jane\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(sibling, []byte("scratch\n"), 0o644))

	require.NoError(t, s.Archive("person", "jane-smith"))

	_, err := os.Stat(folder)
	assert.True(t, os.IsNotExist(err), "live folder should be gone after archive")

	archivedFolder := filepath.Join(s.Root(), s.Schema().Store.ArchiveDir, "people", "jane-smith")
	_, err = os.Stat(filepath.Join(archivedFolder, "index.md"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(archivedFolder, "notes.md"))
	assert.NoError(t, err, "sibling files should travel with the sprawl folder")

	require.NoError(t, s.Restore("person", "jane-smith"))
	_, err = os.Stat(index)
	assert.NoError(t, err)
	_, err = os.Stat(sibling)
	assert.NoError(t, err)
}

// TestArchiveRoundtripFlat covers the flat-opt-out branch: single-file
// archive rename, same as pre-sprawl behavior.
func TestArchiveRoundtripFlat(t *testing.T) {
	s := newFlatTestStore(t)

	src := filepath.Join(s.Root(), "tags", "vip.md")
	require.NoError(t, os.WriteFile(src, []byte("---\nname: vip\n---\n"), 0o644))

	require.NoError(t, s.Archive("tag", "vip"))
	_, err := os.Stat(src)
	assert.True(t, os.IsNotExist(err))

	archived := filepath.Join(s.Root(), s.Schema().Store.ArchiveDir, "tags", "vip.md")
	_, err = os.Stat(archived)
	assert.NoError(t, err)

	require.NoError(t, s.Restore("tag", "vip"))
	_, err = os.Stat(src)
	assert.NoError(t, err)
}

func TestArchiveMissing(t *testing.T) {
	s := newTestStore(t)
	err := s.Archive("person", "does-not-exist")
	assert.True(t, errors.Is(err, ErrNotArchivable))
}

func TestArchiveUnknownKind(t *testing.T) {
	s := newTestStore(t)
	err := s.Archive("alien", "whatever")
	assert.Error(t, err)
}
