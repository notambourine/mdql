// Package wiki provides backlink, orphan, and link-check helpers built on
// top of the markdown store and bleve index.
package wiki

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	_ = ctx
	orphans := make([]Ref, 0)
	for kind, entity := range s.Schema().Entities {
		if entity.AppendOnly {
			continue
		}
		dir, err := s.EntityDir(kind)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			slug := entry.Name()[:len(entry.Name())-len(".md")]
			links, err := idx.Backlinks(slug)
			if err != nil {
				return nil, err
			}
			if len(links) == 0 {
				title, err := orphanTitle(ctx, s, entity, kind, slug)
				if err != nil {
					return nil, err
				}
				orphans = append(orphans, Ref{Type: kind, Slug: slug, Title: title})
			}
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
	known := map[string]map[string]struct{}{}
	for kind, entity := range s.Schema().Entities {
		fullDir := filepath.Join(s.Root(), entity.Dir)
		set := map[string]struct{}{}
		entries, err := os.ReadDir(fullDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", fullDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			slug := entry.Name()[:len(entry.Name())-len(".md")]
			set[slug] = struct{}{}
		}
		known[kind] = set
	}

	slugExists := func(slug string) bool {
		for _, set := range known {
			if _, ok := set[slug]; ok {
				return true
			}
		}
		return false
	}

	dangling := make([]Dangling, 0)
	for kind, entity := range s.Schema().Entities {
		fullDir := filepath.Join(s.Root(), entity.Dir)
		err := walkLinks(fullDir, func(sourceSlug string, links []string) {
			for _, target := range links {
				if !slugExists(target) {
					dangling = append(dangling, Dangling{
						SourceType: kind,
						SourceSlug: sourceSlug,
						TargetSlug: target,
					})
				}
			}
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

func walkLinks(dir string, visit func(sourceSlug string, links []string)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		links := md.ParseLinks(raw)
		if len(links) == 0 {
			continue
		}
		slug := entry.Name()[:len(entry.Name())-len(".md")]
		visit(slug, links)
	}
	return nil
}
