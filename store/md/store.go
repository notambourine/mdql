package md

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/notambourine/mdql/store"
)

// Store is the filesystem-backed implementation.
// Constructed via Open; the schema-driven generic CRUD and Reindex land
// in later commits.
type Store struct {
	root    string
	indexer store.Indexer
}

// Open validates the given root directory and returns a ready Store.
// Use Init first to scaffold a fresh root. If idx is nil, writes are not
// indexed (search returns no results).
func Open(root string, idx store.Indexer) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root %s: %w", root, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}

	if idx == nil {
		idx = store.NoopIndexer{}
	}

	return &Store{root: abs, indexer: idx}, nil
}

// Root returns the absolute path of the store.
func (s *Store) Root() string { return s.root }

// Indexer returns the configured indexer. Exposed so the generic layer
// can emit Upsert/Remove without reaching into Store internals.
func (s *Store) Indexer() store.Indexer { return s.indexer }

// Close is a no-op — the indexer is owned by whoever passed it into Open.
func (s *Store) Close() error { return nil }

// Reindex is a placeholder. The schema-driven implementation that walks
// every kind in schema.Entities lands once the schema package is wired.
func (s *Store) Reindex(ctx context.Context) (ReindexStats, error) {
	_ = ctx
	return ReindexStats{}, nil
}

// ReindexStats reports how many entities were pushed to the indexer.
// Fields are schema-driven once Reindex is wired; for now a flat map
// from kind -> count.
type ReindexStats struct {
	ByKind map[string]int
}
