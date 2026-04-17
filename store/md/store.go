package md

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/notambourine/mdql/schema"
	"github.com/notambourine/mdql/store"
)

// Store is the filesystem-backed implementation.
// Constructed via Open; the schema-driven generic CRUD lands in step 7.
type Store struct {
	root    string
	schema  *schema.Schema
	indexer store.Indexer
}

// Open validates root, attaches the schema, and returns a ready Store.
// Run Init first to scaffold a fresh root. If idx is nil, writes are
// not indexed (search returns no results).
func Open(root string, s *schema.Schema, idx store.Indexer) (*Store, error) {
	if s == nil {
		return nil, fmt.Errorf("open: nil schema")
	}
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

	return &Store{root: abs, schema: s, indexer: idx}, nil
}

// Root returns the absolute path of the store.
func (s *Store) Root() string { return s.root }

// Schema returns the schema the store was opened with.
// Callers iterating kinds should read from here rather than maintain
// their own list.
func (s *Store) Schema() *schema.Schema { return s.schema }

// Indexer returns the configured indexer. Exposed so the generic layer
// can emit Upsert/Remove without reaching into Store internals.
func (s *Store) Indexer() store.Indexer { return s.indexer }

// Close is a no-op — the indexer is owned by whoever passed it into Open.
func (s *Store) Close() error { return nil }

// Reindex walks every schema entity and pushes each record to the
// indexer. Returns counts per kind.
func (s *Store) Reindex(ctx context.Context) (ReindexStats, error) {
	stats := ReindexStats{ByKind: map[string]int{}}
	for kind, entity := range s.schema.Entities {
		records, err := s.List(ctx, kind, nil)
		if err != nil {
			return stats, err
		}
		for _, rec := range records {
			body, _ := rec[bodyKey].(string)
			doc, err := entityDoc(kind, entity, rec, body)
			if err != nil {
				return stats, fmt.Errorf("build doc %s/%s: %w", kind, rec["id"], err)
			}
			if err := s.indexer.Upsert(doc); err != nil {
				return stats, fmt.Errorf("index %s/%s: %w", kind, rec["id"], err)
			}
			stats.ByKind[kind]++
		}
	}
	return stats, nil
}

// ReindexStats reports how many entities were pushed to the indexer,
// keyed by schema entity name.
type ReindexStats struct {
	ByKind map[string]int
}
