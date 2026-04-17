package md

import (
	"regexp"
	"strings"
)

// linkRegex matches wiki-style references [[slug]] where slug is
// lowercase-kebab matching the output of Slugify.
var linkRegex = regexp.MustCompile(`\[\[([a-z0-9][a-z0-9-]*)\]\]`)

// stripWiki returns the bare slug for a value that may be wrapped as
// [[slug]] or already be bare. Whitespace is trimmed. An empty or
// whitespace-only input returns "".
//
// Callers need this because link-field values stored in a generic
// map[string]any bypass model.Link.UnmarshalYAML, so the raw string
// "[[slug]]" reaches the indexer unmodified — which would otherwise
// leave the bleve `links_to` field storing brackets and defeat
// Backlinks queries that use bare slugs.
func stripWiki(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	return s
}

// ParseLinks returns the distinct slugs referenced by [[slug]] patterns
// anywhere in raw. Order of first occurrence is preserved.
//
// Used by the indexer to populate LinksTo for backlink queries, and by
// the wiki check command to find dangling references.
func ParseLinks(raw []byte) []string {
	matches := linkRegex.FindAllSubmatch(raw, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		slug := string(m[1])
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out
}
