package search

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/notambourine/mdql/store"
)

func newTestIndex(t *testing.T) *Index {
	t.Helper()
	path := filepath.Join(t.TempDir(), "idx")
	idx, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func TestUpsertAndSearch(t *testing.T) {
	idx := newTestIndex(t)

	require.NoError(t, idx.Upsert(store.Doc{
		ID: "person:jane", Type: "person", Slug: "jane",
		Title: "Jane Smith", Body: "notes about Jane",
		Tags: []string{"vip", "prospect"},
	}))
	require.NoError(t, idx.Upsert(store.Doc{
		ID: "person:bob", Type: "person", Slug: "bob",
		Title: "Bob Jones", Body: "notes about Bob",
	}))

	results, err := idx.Search("Jane", "", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "person:jane", results[0].ID)
}

func TestSearchTypeFilter(t *testing.T) {
	idx := newTestIndex(t)

	require.NoError(t, idx.Upsert(store.Doc{
		ID: "person:jane", Type: "person", Slug: "jane", Title: "Jane",
	}))
	require.NoError(t, idx.Upsert(store.Doc{
		ID: "organization:acme", Type: "organization", Slug: "acme", Title: "Jane Industries",
	}))

	results, err := idx.Search("Jane", "person", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "person", results[0].Type)
}

func TestBacklinks(t *testing.T) {
	idx := newTestIndex(t)

	require.NoError(t, idx.Upsert(store.Doc{
		ID: "person:jane", Type: "person", Slug: "jane",
		Title:   "Jane",
		LinksTo: []string{"acme", "bob"},
	}))
	require.NoError(t, idx.Upsert(store.Doc{
		ID: "deal:q2", Type: "deal", Slug: "q2",
		Title:   "Q2 Expansion",
		LinksTo: []string{"jane", "acme"},
	}))

	results, err := idx.Backlinks("acme")
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	assert.True(t, ids["person:jane"])
	assert.True(t, ids["deal:q2"])
	assert.Len(t, results, 2)
}

func TestRelsReverse(t *testing.T) {
	idx := newTestIndex(t)

	// Jane stores the edge: mentor → bob.
	require.NoError(t, idx.Upsert(store.Doc{
		ID: "person:jane", Type: "person", Slug: "jane",
		Title: "Jane",
		Rels:  []string{"mentor:bob"},
	}))
	// Alice stores: friend → bob.
	require.NoError(t, idx.Upsert(store.Doc{
		ID: "person:alice", Type: "person", Slug: "alice",
		Title: "Alice",
		Rels:  []string{"friend:bob"},
	}))

	reverse, err := idx.RelsReverse("bob")
	require.NoError(t, err)
	require.Len(t, reverse, 2)

	byType := map[string]string{}
	for _, r := range reverse {
		byType[r.Type] = r.Source
		assert.Equal(t, "reverse", r.Direction)
		assert.Equal(t, "bob", r.Target)
	}
	assert.Equal(t, "jane", byType["mentor"])
	assert.Equal(t, "alice", byType["friend"])
}

func TestRemove(t *testing.T) {
	idx := newTestIndex(t)

	require.NoError(t, idx.Upsert(store.Doc{
		ID: "person:jane", Type: "person", Slug: "jane", Title: "Jane",
	}))

	require.NoError(t, idx.Remove("person:jane"))
	results, err := idx.Search("Jane", "", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestReopenPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idx")

	idx, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, idx.Upsert(store.Doc{
		ID: "person:jane", Type: "person", Slug: "jane", Title: "Jane",
	}))
	require.NoError(t, idx.Close())

	idx2, err := Open(path)
	require.NoError(t, err)
	defer idx2.Close()
	results, err := idx2.Search("Jane", "", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
}
