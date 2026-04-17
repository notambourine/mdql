package store

// Indexer is the write-side contract for the search/backlink index.
//
// Entity stores call Upsert after every successful write and Remove on
// archive. The concrete implementation lives in internal/search; this
// interface lets Step 3 land without depending on Bleve.
type Indexer interface {
	Upsert(Doc) error
	Remove(id string) error
}

// Doc is the shape stored in the search index. Fields are populated by
// entity stores when they marshal a mutation into an index update.
//
// LinksTo captures every [[slug]] reference found in the entity's
// frontmatter string fields and body. Rels captures typed relationship
// edges as "<type>:<target-slug>" so bidirectional queries reduce to a
// term lookup.
type Doc struct {
	ID       string
	Type     string
	Slug     string
	Title    string
	Body     string
	Tags     []string
	LinksTo  []string
	Rels     []string
}

// NoopIndexer discards all writes. Used when search is disabled or during
// tests that don't care about query parity.
type NoopIndexer struct{}

func (NoopIndexer) Upsert(Doc) error    { return nil }
func (NoopIndexer) Remove(string) error { return nil }
