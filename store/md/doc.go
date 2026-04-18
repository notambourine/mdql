package md

import (
	"fmt"
	"sort"

	"github.com/notambourine/mdql/schema"
	"github.com/notambourine/mdql/store"
)

// entityDoc builds the store.Doc for a record against its schema
// definition. Replaces the per-entity personDoc/orgDoc/dealDoc/taskDoc/
// interactionDoc functions from the old typed layer.
//
// Rules derived from schema field types:
//   - a string[] field named "tags" populates Doc.Tags
//   - link       → string appended to Doc.LinksTo
//   - link[]     → all strings appended to Doc.LinksTo
//   - relation[] → "<type>:<to>" appended to Doc.Rels
//
// [[slug]] references inside the body are parsed and merged into
// LinksTo, which is then sorted+deduped so index entries are stable
// across re-indexes.
func entityDoc(kind string, entity schema.Entity, input map[string]any, body string) (store.Doc, error) {
	slug, ok := input["id"].(string)
	if !ok || slug == "" {
		return store.Doc{}, fmt.Errorf("entityDoc: missing id")
	}

	title, err := schema.Render(entity.Title, input)
	if err != nil {
		return store.Doc{}, fmt.Errorf("render title: %w", err)
	}

	var (
		tags  []string
		links []string
		rels  []string
	)
	for fname, field := range entity.Fields {
		switch field.Type {
		case "string[]":
			if fname == "tags" {
				tags = toStringSlice(input[fname])
			}
		case "link":
			// Strip [[...]] wrapping: the generic map[string]any path
			// bypasses model.Link.UnmarshalYAML, so values arrive here
			// as-written (either bare "slug" or wiki-wrapped "[[slug]]").
			// The bleve links_to field must store bare slugs so Backlinks
			// queries (which use bare slugs) can match.
			if v, ok := input[fname].(string); ok {
				if slug := stripWiki(v); slug != "" {
					links = append(links, slug)
				}
			}
		case "link[]":
			for _, v := range toStringSlice(input[fname]) {
				if slug := stripWiki(v); slug != "" {
					links = append(links, slug)
				}
			}
		case "relation[]":
			for _, rel := range toRelationSlice(input[fname]) {
				if rel.Type != "" && rel.To != "" {
					rels = append(rels, rel.Type+":"+rel.To)
					links = append(links, stripWiki(rel.To))
				}
			}
		}
	}

	links = append(links, ParseLinks([]byte(body))...)
	links = expandAll(links)
	links = sortedDedup(links)
	sort.Strings(rels)

	return store.Doc{
		ID:      kind + ":" + slug,
		Type:    kind,
		Slug:    slug,
		Title:   title,
		Body:    body,
		Tags:    append([]string(nil), tags...),
		LinksTo: links,
		Rels:    rels,
	}, nil
}

// relation is the shape of a single relation[] entry on an entity.
// Matches model.Link semantics (type + target slug) without depending
// on that type so this file stays in md without a model import cycle.
type relation struct {
	Type string
	To   string
}

func toStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		out := make([]string, 0, len(x))
		for _, s := range x {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// expandAll fans each link target through ExpandLinkKeys so multi-
// segment refs surface under both their full form and their parent
// slug. The caller is responsible for the final sort+dedup.
func expandAll(links []string) []string {
	if len(links) == 0 {
		return links
	}
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, ExpandLinkKeys(l)...)
	}
	return out
}

func toRelationSlice(v any) []relation {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]relation, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		to, _ := m["to"].(string)
		out = append(out, relation{Type: t, To: to})
	}
	return out
}
