package md

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, Init(root))
	s, err := Open(root, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func strPtr(s string) *string { return &s }
func floatPtr(f float64) *float64 { return &f }
