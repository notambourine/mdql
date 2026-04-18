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

// Ref identifies an entity or sub-file discovered during a wiki
// traversal. ParentKind/ParentSlug/SubKind are populated for sub-file
// hits and empty for entity hits, so a caller can tell which surface
// the reference came from without a second lookup.
type Ref struct {
	Type       string `json:"type"`
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	ParentKind string `json:"parent_kind,omitempty"`
	ParentSlug string `json:"parent_slug,omitempty"`
	SubKind    string `json:"sub_kind,omitempty"`
}

// Dangling is a [[target]] reference whose target can't be resolved.
// SubKind/SubSlug are populated when the source is a sub-file, so the
// locator string printed by `mdql lint` can disambiguate a reference
// inside `projects/X/decisions/Y.md` from one inside `projects/X/index.md`.
type Dangling struct {
	SourceType string `json:"source_type"`
	SourceSlug string `json:"source_slug"`
	SubKind    string `json:"sub_kind,omitempty"`
	SubSlug    string `json:"sub_slug,omitempty"`
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
		refs = append(refs, Ref{
			Type: r.Type, Slug: r.Slug, Title: r.Title,
			ParentKind: r.ParentKind, ParentSlug: r.ParentSlug, SubKind: r.SubKind,
		})
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

// Check scans every entity and sub-file for [[target]] references and
// reports any whose target is unresolved. A target resolves when it
// matches any of three grammars:
//
//	entity slug             — "jane-smith"
//	kind-qualified entity   — "person/jane-smith"
//	sub-file path           — "launch-site/decisions/use-tailwind"
//	                          (parent-slug / sub-file.Dir / sub-slug)
//
// Source attribution follows the same split: references inside a
// sub-file body surface with SubKind/SubSlug set so `mdql lint` can
// point the user at the exact file.
func Check(ctx context.Context, s *md.Store) ([]Dangling, error) {
	_ = ctx
	known := buildKnownSet(s)

	dangling := make([]Dangling, 0)
	for kind, entity := range s.Schema().Entities {
		err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			combined := append(append([]byte{}, front...), body...)
			for _, target := range md.ParseLinks(combined) {
				if _, ok := known[target]; ok {
					continue
				}
				dangling = append(dangling, Dangling{
					SourceType: kind,
					SourceSlug: slug,
					TargetSlug: target,
				})
			}
			if !entity.IsSprawl() || len(entity.Files) == 0 {
				return nil
			}
			return s.ForEachSubFile(kind, slug, "", func(rec md.SubFileRecord, sfFront, sfBody []byte) error {
				combined := append(append([]byte{}, sfFront...), sfBody...)
				for _, target := range md.ParseLinks(combined) {
					if _, ok := known[target]; ok {
						continue
					}
					dangling = append(dangling, Dangling{
						SourceType: kind,
						SourceSlug: slug,
						SubKind:    rec.Kind,
						SubSlug:    rec.Slug,
						TargetSlug: target,
					})
				}
				return nil
			})
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
		if dangling[i].SubKind != dangling[j].SubKind {
			return dangling[i].SubKind < dangling[j].SubKind
		}
		if dangling[i].SubSlug != dangling[j].SubSlug {
			return dangling[i].SubSlug < dangling[j].SubSlug
		}
		return dangling[i].TargetSlug < dangling[j].TargetSlug
	})
	return dangling, nil
}

// buildKnownSet enumerates every resolvable link target in the store:
// each entity slug and its "kind/slug" form, and every sub-file path
// of the form "parent-slug/sub-dir/sub-slug". Used by Check to decide
// whether a [[target]] reference is dangling.
func buildKnownSet(s *md.Store) map[string]struct{} {
	known := map[string]struct{}{}
	for kind, entity := range s.Schema().Entities {
		_ = s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			known[slug] = struct{}{}
			known[kind+"/"+slug] = struct{}{}
			if !entity.IsSprawl() || len(entity.Files) == 0 {
				return nil
			}
			for subKind, sub := range entity.Files {
				_ = s.ForEachSubFile(kind, slug, subKind, func(rec md.SubFileRecord, _, _ []byte) error {
					known[slug+"/"+sub.Dir+"/"+rec.Slug] = struct{}{}
					return nil
				})
			}
			return nil
		})
	}
	return known
}
