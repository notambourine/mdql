package md

import (
	"context"
	"sort"
)

// TagCount is the aggregate surfaced by ListTags.
type TagCount struct {
	Name  string `json:"name" yaml:"name"`
	Count int    `json:"count" yaml:"count"`
}

// AddTag applies a tag to the entity identified by kind+slug.
// Idempotent: re-applying an existing tag is a no-op on disk.
func (s *Store) AddTag(ctx context.Context, kind, slug, tag string) error {
	return s.mutateTags(ctx, kind, slug, func(tags []string) []string {
		return sortedDedup(append(tags, tag))
	})
}

// RemoveTag strips a tag from the entity identified by kind+slug.
// No-op if the tag isn't present.
func (s *Store) RemoveTag(ctx context.Context, kind, slug, tag string) error {
	return s.mutateTags(ctx, kind, slug, func(tags []string) []string {
		out := tags[:0]
		for _, t := range tags {
			if t != tag {
				out = append(out, t)
			}
		}
		return sortedDedup(out)
	})
}

// ListTags returns every distinct tag across all schema-declared
// entities with a usage count. Used by the generic `tag list` command.
func (s *Store) ListTags(ctx context.Context) ([]TagCount, error) {
	counts := make(map[string]int)
	for kind := range s.schema.Entities {
		err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			if err := ctxErr(ctx); err != nil {
				return err
			}
			for _, t := range extractTags(front) {
				counts[t]++
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]TagCount, 0, len(counts))
	for name, count := range counts {
		out = append(out, TagCount{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// TagsFor returns the tags on a specific entity.
func (s *Store) TagsFor(ctx context.Context, kind, slug string) ([]string, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	path, err := s.EntityPath(kind, slug)
	if err != nil {
		return nil, err
	}
	front, _, err := Parse(path)
	if err != nil {
		return nil, err
	}
	return extractTags(front), nil
}

// DeleteTag removes a tag from every entity that has it. Returns the
// count of files modified. Callers should gate on the count to guard
// against accidental mass edits.
func (s *Store) DeleteTag(ctx context.Context, tag string) (int, error) {
	modified := 0
	for kind := range s.schema.Entities {
		var slugs []string
		err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			if err := ctxErr(ctx); err != nil {
				return err
			}
			for _, t := range extractTags(front) {
				if t == tag {
					slugs = append(slugs, slug)
					return nil
				}
			}
			return nil
		})
		if err != nil {
			return modified, err
		}
		for _, slug := range slugs {
			if err := s.RemoveTag(ctx, kind, slug, tag); err != nil {
				return modified, err
			}
			modified++
		}
	}
	return modified, nil
}

// CountTagUsage returns the number of entities currently tagged with tag.
// Used by CLI to decide whether to prompt for --force.
func (s *Store) CountTagUsage(ctx context.Context, tag string) (int, error) {
	count := 0
	for kind := range s.schema.Entities {
		err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			if err := ctxErr(ctx); err != nil {
				return err
			}
			for _, t := range extractTags(front) {
				if t == tag {
					count++
					break
				}
			}
			return nil
		})
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

// mutateTags reads, mutates, and re-writes the entity's tags array.
// Delegates to Update so the indexer stays in sync and updated_at
// advances (uniform with every other write path; lighter-weight split
// from the old typed layer didn't earn its complexity).
func (s *Store) mutateTags(ctx context.Context, kind, slug string, mutate func([]string) []string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	rec, err := s.Get(ctx, kind, slug)
	if err != nil {
		return err
	}
	current := toStringSlice(rec["tags"])
	next := mutate(current)
	if stringSlicesEqual(current, next) {
		return nil
	}
	_, err = s.Update(ctx, kind, slug, map[string]any{"tags": next})
	return err
}

// extractTags pulls the `tags:` array out of raw frontmatter bytes
// without decoding the whole entity. Returns empty slice when absent.
func extractTags(front []byte) []string {
	var doc struct {
		Tags []string `yaml:"tags"`
	}
	_ = decodeInto(front, &doc)
	return doc.Tags
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

