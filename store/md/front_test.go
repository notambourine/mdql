package md

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBytes(t *testing.T) {
	in := []byte("---\nname: Jane\nage: 30\n---\n\nHello [[bob]]\n")
	front, body, err := ParseBytes(in)
	require.NoError(t, err)
	assert.Equal(t, "name: Jane\nage: 30\n", string(front))
	// Parse normalizes trailing newlines so the in-memory body is the
	// "business" string without file-formatting artifacts.
	assert.Equal(t, "Hello [[bob]]", string(body))
}

func TestParseBytesNoFrontmatter(t *testing.T) {
	in := []byte("just a body\n")
	front, body, err := ParseBytes(in)
	require.NoError(t, err)
	assert.Empty(t, front)
	// No frontmatter — body passes through without trimming (there's no
	// split to restore). Documented behavior: only frontmatter-split
	// bodies get trailing-newline normalization.
	assert.Equal(t, "just a body\n", string(body))
}

func TestParseBytesUnterminated(t *testing.T) {
	in := []byte("---\nname: Jane\n")
	_, _, err := ParseBytes(in)
	assert.Error(t, err)
}

func TestWriteRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	front := []byte("name: Jane\ntags: [a, b]\n")
	body := "Hello [[bob]]"

	require.NoError(t, Write(path, front, []byte(body)))

	gotFront, gotBody, err := Parse(path)
	require.NoError(t, err)
	assert.Equal(t, string(front), string(gotFront))
	assert.Equal(t, body, string(gotBody))
}

func TestWriteEmptyBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	front := []byte("id: x\n")

	require.NoError(t, Write(path, front, nil))

	gotFront, gotBody, err := Parse(path)
	require.NoError(t, err)
	assert.Equal(t, string(front), string(gotFront))
	assert.Empty(t, gotBody)
}

func TestWriteAtomicTempCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	require.NoError(t, Write(path, []byte("id: x\n"), nil))

	// Temp file should not linger after a successful write.
	tmp := path + ".tmp"
	_, _, err := Parse(tmp)
	assert.Error(t, err)
}
