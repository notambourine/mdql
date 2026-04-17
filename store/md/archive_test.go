package md

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveRoundtrip(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Init(root))

	src := filepath.Join(root, "people", "jane-smith.md")
	require.NoError(t, os.WriteFile(src, []byte("---\nid: jane-smith\n---\n"), 0o644))

	require.NoError(t, Archive(root, KindPerson, "jane-smith"))

	// Source gone.
	_, err := os.Stat(src)
	assert.True(t, os.IsNotExist(err))

	// Archived copy present.
	archived := filepath.Join(root, ArchiveDir, "people", "jane-smith.md")
	_, err = os.Stat(archived)
	assert.NoError(t, err)

	// Restore brings it back.
	require.NoError(t, Restore(root, KindPerson, "jane-smith"))
	_, err = os.Stat(src)
	assert.NoError(t, err)
}

func TestArchiveMissing(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Init(root))
	err := Archive(root, KindPerson, "does-not-exist")
	assert.True(t, errors.Is(err, ErrNotArchivable))
}

func TestArchiveUnknownKind(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Init(root))
	err := Archive(root, "alien", "whatever")
	assert.Error(t, err)
}
