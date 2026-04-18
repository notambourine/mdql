// Package search is the Bleve-backed FTS + backlink index for crm-cli.
//
// The same index powers three queries:
//   - Search: full-text over title/body/tags, optionally filtered by type.
//   - Backlinks: reverse lookup of [[slug]] references.
//   - RelsFor: forward + reverse person-to-person relationship edges
//     reconstructed from one-sided storage.
package search

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/notambourine/mdql/store"
)

// Index is a Bleve-backed FTS + link index. Implements store.Indexer.
type Index struct {
	idx  bleve.Index
	path string
}

// Open loads or creates a Bleve index at path. path is typically
// <root>/.crm/index — callers should pass an absolute path.
func Open(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir index parent: %w", err)
	}
	idx, err := bleve.Open(path)
	if err != nil {
		if !errors.Is(err, bleve.ErrorIndexPathDoesNotExist) {
			return nil, fmt.Errorf("open index: %w", err)
		}
		idx, err = bleve.New(path, buildMapping())
		if err != nil {
			return nil, fmt.Errorf("create index: %w", err)
		}
	}
	return &Index{idx: idx, path: path}, nil
}

// Close releases the underlying Bleve handle.
func (i *Index) Close() error {
	if i.idx == nil {
		return nil
	}
	return i.idx.Close()
}

// Upsert indexes (or replaces) a document.
func (i *Index) Upsert(doc store.Doc) error {
	payload := map[string]any{
		"type":        doc.Type,
		"slug":        doc.Slug,
		"title":       doc.Title,
		"body":        doc.Body,
		"tags":        doc.Tags,
		"links_to":    doc.LinksTo,
		"rels":        doc.Rels,
		"rel_target":  relTargets(doc.Rels),
		"parent_kind": doc.ParentKind,
		"parent_slug": doc.ParentSlug,
		"sub_kind":    doc.SubKind,
	}
	return i.idx.Index(doc.ID, payload)
}

// Remove deletes a document from the index. id is the composite
// "<type>:<slug>" used by Upsert.
func (i *Index) Remove(id string) error {
	return i.idx.Delete(id)
}

func buildMapping() mapping.IndexMapping {
	m := bleve.NewIndexMapping()
	entity := bleve.NewDocumentMapping()

	// Full-text fields: standard analyzer on title and body.
	text := bleve.NewTextFieldMapping()
	text.Store = true
	entity.AddFieldMappingsAt("title", text)
	entity.AddFieldMappingsAt("body", text)

	// Keyword fields: exact-match, stored so we can retrieve hits without
	// re-reading the file.
	kw := bleve.NewKeywordFieldMapping()
	kw.Store = true
	for _, field := range []string{"type", "slug", "tags", "links_to", "rels", "rel_target", "parent_kind", "parent_slug", "sub_kind"} {
		entity.AddFieldMappingsAt(field, kw)
	}

	m.DefaultMapping = entity
	return m
}

// relTargets extracts just the target slugs from "<type>:<slug>" rel
// strings, so reverse lookups can term-query a clean slug field.
func relTargets(rels []string) []string {
	if len(rels) == 0 {
		return nil
	}
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		for n := len(r) - 1; n >= 0; n-- {
			if r[n] == ':' {
				out = append(out, r[n+1:])
				break
			}
		}
	}
	return out
}
