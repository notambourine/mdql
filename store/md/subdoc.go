package md

import (
	"fmt"
	"sort"

	"github.com/notambourine/mdql/schema"
	"github.com/notambourine/mdql/store"
)

// subFileDoc builds the store.Doc for a sub-file record. Differs from
// entityDoc in three ways:
//   - ID namespaces the parent: "<parentKind>:<parentSlug>:<subKind>:<subSlug>".
//     Keeps the bleve namespace collision-free when two parents each
//     host a sub-file with the same slug ("kickoff" meeting).
//   - Type carries the sub-file kind so `search --type meeting` hits
//     meeting sub-files regardless of parent kind.
//   - LinksTo always includes the parent slug so `wiki backlinks <parent>`
//     surfaces the sub-file as an inbound reference from the parent
//     surface — keeping the parent reachable from the graph even when
//     the sub-file body only refers back to its own siblings.
func subFileDoc(parentKind, parentSlug string, sub schema.SubFile, rec SubFileRecord) (store.Doc, error) {
	if rec.Slug == "" {
		return store.Doc{}, fmt.Errorf("subFileDoc: missing slug")
	}
	title, err := renderSubFileTitle(sub, rec.Fields)
	if err != nil {
		return store.Doc{}, err
	}

	var (
		tags  []string
		links []string
		rels  []string
	)
	for fname, field := range sub.Fields {
		switch field.Type {
		case "string[]":
			if fname == "tags" {
				tags = toStringSlice(rec.Fields[fname])
			}
		case "link":
			if v, ok := rec.Fields[fname].(string); ok {
				if target := stripWiki(v); target != "" {
					links = append(links, target)
				}
			}
		case "link[]":
			for _, v := range toStringSlice(rec.Fields[fname]) {
				if target := stripWiki(v); target != "" {
					links = append(links, target)
				}
			}
		case "relation[]":
			for _, rel := range toRelationSlice(rec.Fields[fname]) {
				if rel.Type != "" && rel.To != "" {
					rels = append(rels, rel.Type+":"+rel.To)
					links = append(links, stripWiki(rel.To))
				}
			}
		}
	}

	links = append(links, ParseLinks([]byte(rec.Body))...)
	links = append(links, parentSlug)
	links = expandAll(links)
	links = sortedDedup(links)
	sort.Strings(rels)

	return store.Doc{
		ID:         parentKind + ":" + parentSlug + ":" + rec.Kind + ":" + rec.Slug,
		Type:       rec.Kind,
		Slug:       rec.Slug,
		Title:      title,
		Body:       rec.Body,
		Tags:       append([]string(nil), tags...),
		LinksTo:    links,
		Rels:       rels,
		ParentKind: parentKind,
		ParentSlug: parentSlug,
		SubKind:    rec.Kind,
	}, nil
}

// renderSubFileTitle falls back to the sub-file slug when the schema
// defines no title template — sub-files often have terse schemas where
// the slug already carries the readable identity (e.g. date-subject).
func renderSubFileTitle(sub schema.SubFile, fields map[string]any) (string, error) {
	if sub.Title == "" {
		return "", nil
	}
	t, err := schema.Render(sub.Title, fields)
	if err != nil {
		return "", fmt.Errorf("render sub-file title: %w", err)
	}
	return t, nil
}

// subFileDocID is the canonical id used when removing a sub-file from
// the index. Kept in one place so Delete and Upsert can't drift.
func subFileDocID(parentKind, parentSlug, subKind, subSlug string) string {
	return parentKind + ":" + parentSlug + ":" + subKind + ":" + subSlug
}
