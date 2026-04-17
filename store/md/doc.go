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
	uuid, _ := input["uuid"].(string)

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
			if v, ok := input[fname].(string); ok && v != "" {
				links = append(links, v)
			}
		case "link[]":
			links = append(links, toStringSlice(input[fname])...)
		case "relation[]":
			for _, rel := range toRelationSlice(input[fname]) {
				if rel.Type != "" && rel.To != "" {
					rels = append(rels, rel.Type+":"+rel.To)
					links = append(links, rel.To)
				}
			}
		}
	}

	links = append(links, ParseLinks([]byte(body))...)
	links = sortedDedup(links)
	sort.Strings(rels)

	return store.Doc{
		ID:      kind + ":" + slug,
		Type:    kind,
		Slug:    slug,
		UUID:    uuid,
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
