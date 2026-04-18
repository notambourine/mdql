package engine

import (
	"bytes"
	"strings"
	"testing"
)

// TestPrintOnboarding pins the contract that the onboarding goldenset
// surfaces every feature name an agent needs to derive commands.
// If this test fails, the embedded onboarding.md has drifted away from
// the actual CLI surface — fix the doc, don't relax the test.
func TestPrintOnboarding(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintOnboarding(&buf); err != nil {
		t.Fatalf("PrintOnboarding: %v", err)
	}
	out := buf.String()
	if len(out) < 1000 {
		t.Fatalf("onboarding doc suspiciously short: %d bytes", len(out))
	}
	for _, want := range []string{
		"schema.yml",
		"examples/crm/",
		"examples/zettelkasten/",
		"examples/tasks/",
		"string[]",
		"enum",
		"link",
		"[[slug]]",
		"--dry-run",
		"--fields",
		"--sub",
		"wiki backlinks",
		"wiki orphans",
		"wiki dangling",
		"schema describe --format json",
		"Bootstrapping",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("onboarding doc missing required marker %q", want)
		}
	}
}
