package search

import (
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// Result is a flattened search hit for CLI consumption.
type Result struct {
	ID    string  `json:"id"`
	Type  string  `json:"type"`
	Slug  string  `json:"slug"`
	UUID  string  `json:"uuid"`
	Title string  `json:"title"`
	Score float64 `json:"score"`
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

// Search runs a full-text query against title+body+tags.
// typeFilter narrows to a single entity kind when non-empty.
func (i *Index) Search(q, typeFilter string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 20
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}

	var searchQuery query.Query
	matchAll := bleve.NewQueryStringQuery(q)
	if typeFilter == "" {
		searchQuery = matchAll
	} else {
		typeClause := bleve.NewTermQuery(typeFilter)
		typeClause.SetField("type")
		searchQuery = bleve.NewConjunctionQuery(matchAll, typeClause)
	}

	req := bleve.NewSearchRequestOptions(searchQuery, limit, 0, false)
	req.Fields = []string{"type", "slug", "uuid", "title"}

	res, err := i.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	out := make([]Result, 0, len(res.Hits))
	for _, hit := range res.Hits {
		out = append(out, Result{
			ID:    hit.ID,
			Type:  stringField(hit.Fields, "type"),
			Slug:  stringField(hit.Fields, "slug"),
			UUID:  stringField(hit.Fields, "uuid"),
			Title: stringField(hit.Fields, "title"),
			Score: hit.Score,
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
	req.Fields = []string{"type", "slug", "uuid", "title"}

	res, err := i.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("backlinks: %w", err)
	}
	out := make([]Result, 0, len(res.Hits))
	for _, hit := range res.Hits {
		out = append(out, Result{
			ID:    hit.ID,
			Type:  stringField(hit.Fields, "type"),
			Slug:  stringField(hit.Fields, "slug"),
			UUID:  stringField(hit.Fields, "uuid"),
			Title: stringField(hit.Fields, "title"),
			Score: hit.Score,
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
