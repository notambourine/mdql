package search

import (
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// Result is a flattened search hit for CLI consumption. ParentKind,
// ParentSlug, and SubKind are populated for sub-file hits and empty
// for entity hits, so the JSON shape stays uniform across kinds.
type Result struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Slug       string  `json:"slug"`
	Title      string  `json:"title"`
	Score      float64 `json:"score"`
	ParentKind string  `json:"parent_kind,omitempty"`
	ParentSlug string  `json:"parent_slug,omitempty"`
	SubKind    string  `json:"sub_kind,omitempty"`
}

// RelResult describes a single person-to-person relationship edge.
//
// Direction is "forward" when the owning file stores the edge directly,
// and "reverse" when the edge was found by scanning other entities'
// relationships for a pointer at the queried slug.
type RelResult struct {
	Source    string `json:"source"`
	Type      string `json:"type"`
	Target    string `json:"target"`
	Direction string `json:"direction"`
}

// Filter narrows a Search call by record kind. Zero-value means
// "no filter on that axis". --type is a disjunction that matches either
// the stored type (entity docs) or sub_kind (sub-file docs) — the usual
// case when a user knows the kind name and doesn't care which surface
// it lives on. ParentKind + SubKind are the explicit-compound form, for
// the rare case where a sub-kind name collides with an entity kind.
type Filter struct {
	Type       string
	ParentKind string
	SubKind    string
}

// Search runs a full-text query against title+body+tags.
// Filter narrows results to the specified kind axes; any Zero field on
// Filter is ignored.
func (i *Index) Search(q string, filter Filter, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 20
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}

	clauses := []query.Query{bleve.NewQueryStringQuery(q)}
	if filter.Type != "" {
		typeOr := bleve.NewTermQuery(filter.Type)
		typeOr.SetField("type")
		subOr := bleve.NewTermQuery(filter.Type)
		subOr.SetField("sub_kind")
		clauses = append(clauses, bleve.NewDisjunctionQuery(typeOr, subOr))
	}
	if filter.ParentKind != "" {
		parent := bleve.NewTermQuery(filter.ParentKind)
		parent.SetField("parent_kind")
		clauses = append(clauses, parent)
	}
	if filter.SubKind != "" {
		sub := bleve.NewTermQuery(filter.SubKind)
		sub.SetField("sub_kind")
		clauses = append(clauses, sub)
	}
	var searchQuery query.Query
	if len(clauses) == 1 {
		searchQuery = clauses[0]
	} else {
		searchQuery = bleve.NewConjunctionQuery(clauses...)
	}

	req := bleve.NewSearchRequestOptions(searchQuery, limit, 0, false)
	req.Fields = []string{"type", "slug", "title", "parent_kind", "parent_slug", "sub_kind"}

	res, err := i.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	out := make([]Result, 0, len(res.Hits))
	for _, hit := range res.Hits {
		out = append(out, Result{
			ID:         hit.ID,
			Type:       stringField(hit.Fields, "type"),
			Slug:       stringField(hit.Fields, "slug"),
			Title:      stringField(hit.Fields, "title"),
			Score:      hit.Score,
			ParentKind: stringField(hit.Fields, "parent_kind"),
			ParentSlug: stringField(hit.Fields, "parent_slug"),
			SubKind:    stringField(hit.Fields, "sub_kind"),
		})
	}
	return out, nil
}

// Backlinks returns entities whose frontmatter or body references
// [[slug]]. Used by wiki backlinks and by the context briefing.
func (i *Index) Backlinks(slug string) ([]Result, error) {
	termQ := bleve.NewTermQuery(slug)
	termQ.SetField("links_to")
	req := bleve.NewSearchRequestOptions(termQ, 1000, 0, false)
	req.Fields = []string{"type", "slug", "title", "parent_kind", "parent_slug", "sub_kind"}

	res, err := i.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("backlinks: %w", err)
	}
	out := make([]Result, 0, len(res.Hits))
	for _, hit := range res.Hits {
		out = append(out, Result{
			ID:         hit.ID,
			Type:       stringField(hit.Fields, "type"),
			Slug:       stringField(hit.Fields, "slug"),
			Title:      stringField(hit.Fields, "title"),
			Score:      hit.Score,
			ParentKind: stringField(hit.Fields, "parent_kind"),
			ParentSlug: stringField(hit.Fields, "parent_slug"),
			SubKind:    stringField(hit.Fields, "sub_kind"),
		})
	}
	return out, nil
}

// RelsReverse returns relationship edges pointing at slug. Forward edges
// (slug's own relationships) must be read from its frontmatter by the
// caller — the store layer owns that lookup.
func (i *Index) RelsReverse(slug string) ([]RelResult, error) {
	termQ := bleve.NewTermQuery(slug)
	termQ.SetField("rel_target")
	req := bleve.NewSearchRequestOptions(termQ, 1000, 0, false)
	req.Fields = []string{"slug", "rels"}

	res, err := i.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("rels reverse: %w", err)
	}
	out := []RelResult{}
	for _, hit := range res.Hits {
		sourceSlug := stringField(hit.Fields, "slug")
		rels := stringSliceField(hit.Fields, "rels")
		for _, rel := range rels {
			if idx := strings.LastIndex(rel, ":"); idx > 0 && rel[idx+1:] == slug {
				out = append(out, RelResult{
					Source:    sourceSlug,
					Type:      rel[:idx],
					Target:    slug,
					Direction: "reverse",
				})
			}
		}
	}
	return out, nil
}

func stringField(fields map[string]any, key string) string {
	if v, ok := fields[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func stringSliceField(fields map[string]any, key string) []string {
	v, ok := fields[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
