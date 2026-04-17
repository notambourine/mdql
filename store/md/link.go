package md

import "regexp"

// linkRegex matches wiki-style references [[slug]] where slug is
// lowercase-kebab matching the output of Slugify.
var linkRegex = regexp.MustCompile(`\[\[([a-z0-9][a-z0-9-]*)\]\]`)

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
