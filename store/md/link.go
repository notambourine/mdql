package md

import (
	"regexp"
	"strings"
)

// linkRegex matches wiki-style references. Three grammars land here:
//
//	[[slug]]                            — bare entity slug
//	[[kind/slug]]                       — kind-qualified entity
//	[[parent-slug/subdir/sub-slug]]     — sub-file path
//
// Each segment is a lowercase-kebab token (Slugify output). Slashes
// separate segments; no leading/trailing slash. Arbitrary depth is
// permitted so callers aren't bound to three — unknown segment counts
// simply fall through as "dangling" when resolved.
var linkRegex = regexp.MustCompile(`\[\[([a-z0-9][a-z0-9-]*(?:/[a-z0-9][a-z0-9-]*)*)\]\]`)

// stripWiki returns the bare target for a value that may be wrapped as
// [[target]] or already be bare. Whitespace is trimmed. An empty or
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

// ParseLinks returns the distinct link targets referenced by [[...]]
// patterns anywhere in raw. Targets may contain `/` segments (sub-file
// paths, kind-qualified refs). Order of first occurrence is preserved.
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
		target := string(m[1])
		if _, dup := seen[target]; dup {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

// ExpandLinkKeys returns the set of bleve `links_to` keys to index for
// one wiki target. Multi-segment targets fan out so that `backlinks X`
// surfaces both specific refs to X and sub-file refs inside X:
//
//	"jane-smith"                           → ["jane-smith"]
//	"person/jane-smith"                    → ["person/jane-smith",
//	                                          "person", "jane-smith"]
//	"launch-site/decisions/use-tailwind"   → ["launch-site/decisions/use-tailwind",
//	                                          "launch-site"]
//
// The full form is always first (preserved order). The first segment is
// always included so `backlinks <parent>` finds sub-file refs into the
// parent. For two-segment refs the last segment is also included so
// `backlinks <slug>` still resolves when callers used the kind-qualified
// form in the body.
func ExpandLinkKeys(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "/")
	out := []string{raw}
	if len(parts) < 2 {
		return out
	}
	seen := map[string]struct{}{raw: {}}
	add := func(k string) {
		if k == "" {
			return
		}
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	add(parts[0])
	if len(parts) == 2 {
		add(parts[1])
	}
	return out
}
