// Package wiki provides backlink, orphan, and link-check helpers built on
// top of the markdown store and bleve index.
package wiki

import (
	"context"
	"sort"

	"github.com/notambourine/mdql/schema"
	"github.com/notambourine/mdql/search"
	"github.com/notambourine/mdql/store/md"
)

// Ref identifies an entity discovered during a wiki traversal.
type Ref struct {
	Type  string `json:"type"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Dangling is a [[slug]] reference whose target file doesn't exist.
type Dangling struct {
	SourceType string `json:"source_type"`
	SourceSlug string `json:"source_slug"`
	TargetSlug string `json:"target_slug"`
}

// Backlinks returns entities that reference [[slug]] anywhere in their
// frontmatter or body. Thin wrapper over the search index's backlinks.
func Backlinks(idx *search.Index, slug string) ([]Ref, error) {
	results, err := idx.Backlinks(slug)
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(results))
	for _, r := range results {
		refs = append(refs, Ref{Type: r.Type, Slug: r.Slug, Title: r.Title})
	}
	return refs, nil
}

// Orphans returns entities whose slug appears nowhere in other entities'
// links. An orphan is a node with no inbound references.
//
// Skips entities declared append_only in the schema: those (e.g. an
// interaction log) are "attached" via their own link fields, so an
// orphan here would be a meaningful concept only when the entity has
// zero links, which is already surfaced by listing it directly.
func Orphans(ctx context.Context, s *md.Store, idx *search.Index) ([]Ref, error) {
	orphans := make([]Ref, 0)
	for kind, entity := range s.Schema().Entities {
		if entity.AppendOnly {
			continue
		}
		err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			links, err := idx.Backlinks(slug)
			if err != nil {
				return err
			}
			if len(links) > 0 {
				return nil
			}
			title, err := orphanTitle(ctx, s, entity, kind, slug)
			if err != nil {
				return err
			}
			orphans = append(orphans, Ref{Type: kind, Slug: slug, Title: title})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Type != orphans[j].Type {
			return orphans[i].Type < orphans[j].Type
		}
		return orphans[i].Slug < orphans[j].Slug
	})
	return orphans, nil
}

// orphanTitle renders the entity's title template against the record
// on disk. Returns "" when the record can't be read — the caller still
// emits the orphan with an empty title rather than surfacing a read
// error, so a single malformed file doesn't mask the whole report.
func orphanTitle(ctx context.Context, s *md.Store, entity schema.Entity, kind, slug string) (string, error) {
	rec, err := s.Get(ctx, kind, slug)
	if err != nil {
		return "", nil
	}
	title, err := schema.Render(entity.Title, rec)
	if err != nil {
		return "", nil
	}
	return title, nil
}

// Check scans every entity for [[slug]] references and reports any whose
// target file doesn't exist in any schema-declared entity directory.
func Check(ctx context.Context, s *md.Store) ([]Dangling, error) {
	_ = ctx
	known := map[string]struct{}{}
	for kind := range s.Schema().Entities {
		err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			known[slug] = struct{}{}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	dangling := make([]Dangling, 0)
	for kind := range s.Schema().Entities {
		err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			links := md.ParseLinks(append(append([]byte{}, front...), body...))
			for _, target := range links {
				if _, ok := known[target]; ok {
					continue
				}
				dangling = append(dangling, Dangling{
					SourceType: kind,
					SourceSlug: slug,
					TargetSlug: target,
				})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(dangling, func(i, j int) bool {
		if dangling[i].SourceType != dangling[j].SourceType {
			return dangling[i].SourceType < dangling[j].SourceType
		}
		if dangling[i].SourceSlug != dangling[j].SourceSlug {
			return dangling[i].SourceSlug < dangling[j].SourceSlug
		}
		return dangling[i].TargetSlug < dangling[j].TargetSlug
	})
	return dangling, nil
}
