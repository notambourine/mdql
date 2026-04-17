package md

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLinks(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"none", "no links here", nil},
		{"one", "ref to [[jane-smith]]", []string{"jane-smith"}},
		{"multiple", "[[a]] and [[b]] and [[a]]", []string{"a", "b"}},
		{"frontmatter", "---\norg: \"[[acme-corp]]\"\n---\n", []string{"acme-corp"}},
		{"rejects uppercase", "[[Jane-Smith]]", nil},
		{"rejects spaces", "[[jane smith]]", nil},
		{"rejects underscore", "[[jane_smith]]", nil},
		{"leading digit ok", "[[2026-q2-deal]]", []string{"2026-q2-deal"}},
		{"nested brackets", "[[[wrapped]]]", []string{"wrapped"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLinks([]byte(tt.in))
			assert.Equal(t, tt.want, got)
		})
	}
}
