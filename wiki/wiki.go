// Package wiki provides backlink, orphan, and link-check helpers built on
// top of the markdown store and bleve index.
package wiki

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
// Interactions are always "attached" via their people field, so we skip
// them — an orphan interaction is a meaningful concept only if it has
// zero people, which is already surfaced by listing interactions.
func Orphans(ctx context.Context, s *md.Store, idx *search.Index) ([]Ref, error) {
	kinds := []string{md.KindPerson, md.KindOrg, md.KindDeal, md.KindTask}
	var orphans []Ref
	for _, kind := range kinds {
		dir, err := md.EntityDir(s.Root(), kind)
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
				orphans = append(orphans, Ref{Type: kind, Slug: slug})
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

// Check scans every entity for [[slug]] references and reports any whose
// target file doesn't exist in the expected directories.
func Check(ctx context.Context, s *md.Store) ([]Dangling, error) {
	root := s.Root()
	known := map[string]map[string]struct{}{}
	for kind, dir := range md.Dirs {
		fullDir := filepath.Join(root, dir)
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

	var dangling []Dangling
	for kind, dir := range md.Dirs {
		fullDir := filepath.Join(root, dir)
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
