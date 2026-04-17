package model

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// Link is a wiki-style reference to another entity, identified by slug.
//
// On disk the value is serialized as "[[slug]]" in YAML frontmatter and as
// the bare slug in JSON CLI output. The zero value ("") means "not set".
type Link string

// Slug returns the raw slug ("jane-smith").
func (l Link) Slug() string { return string(l) }

// Wiki returns the wiki-wrapped form ("[[jane-smith]]").
// Returns empty string when the link is unset.
func (l Link) Wiki() string {
	if l == "" {
		return ""
	}
	return "[[" + string(l) + "]]"
}

// MarshalYAML writes links as "[[slug]]" in frontmatter.
func (l Link) MarshalYAML() (any, error) {
	if l == "" {
		return nil, nil
	}
	return "[[" + string(l) + "]]", nil
}

// UnmarshalYAML accepts either "[[slug]]" or a bare "slug".
func (l *Link) UnmarshalYAML(n *yaml.Node) error {
	*l = Link(stripWiki(n.Value))
	return nil
}

// MarshalJSON writes the bare slug so CLI JSON output stays ergonomic.
func (l Link) MarshalJSON() ([]byte, error) {
	if l == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(l))
}

// UnmarshalJSON accepts either "[[slug]]" or a bare "slug".
func (l *Link) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*l = Link(stripWiki(s))
	return nil
}

func stripWiki(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	return s
}
