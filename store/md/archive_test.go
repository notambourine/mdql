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
	s := newTestStore(t)

	src := filepath.Join(s.Root(), "people", "jane-smith.md")
	require.NoError(t, os.WriteFile(src, []byte("---\nid: jane-smith\n---\n"), 0o644))

	require.NoError(t, s.Archive("person", "jane-smith"))

	// Source gone.
	_, err := os.Stat(src)
	assert.True(t, os.IsNotExist(err))

	// Archived copy present.
	archived := filepath.Join(s.Root(), s.Schema().Store.ArchiveDir, "people", "jane-smith.md")
	_, err = os.Stat(archived)
	assert.NoError(t, err)

	// Restore brings it back.
	require.NoError(t, s.Restore("person", "jane-smith"))
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
