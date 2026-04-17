package md

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Jane Smith", "jane-smith"},
		{"  Bob  Jones  ", "bob-jones"},
		{"Acme Corp!", "acme-corp"},
		{"O'Reilly Media", "oreilly-media"},
		{"UPPER_snake_case", "upper-snake-case"},
		{"multiple---dashes", "multiple-dashes"},
		{"123 Numbers", "123-numbers"},
		{"", ""},
		{"!!!", ""},
		{"café", "caf"}, // documented ASCII-only limitation
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, Slugify(tt.in))
		})
	}
}

func TestEnsureUnique(t *testing.T) {
	dir := t.TempDir()

	// First slug is free.
	got, err := EnsureUnique(dir, "jane-smith")
	require.NoError(t, err)
	assert.Equal(t, "jane-smith", got)

	// Write the file; next call gets -2.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "jane-smith.md"), nil, 0o644))
	got, err = EnsureUnique(dir, "jane-smith")
	require.NoError(t, err)
	assert.Equal(t, "jane-smith-2", got)

	// Write -2 too; next call gets -3.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "jane-smith-2.md"), nil, 0o644))
	got, err = EnsureUnique(dir, "jane-smith")
	require.NoError(t, err)
	assert.Equal(t, "jane-smith-3", got)
}

func TestEnsureUniqueEmptySlug(t *testing.T) {
	_, err := EnsureUnique(t.TempDir(), "")
	assert.ErrorIs(t, err, ErrEmptySlug)
}
