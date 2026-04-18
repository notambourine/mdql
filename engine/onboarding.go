package engine

import (
	_ "embed"
	"io"
)

//go:embed onboarding.md
var onboardingDoc string

// PrintOnboarding writes the no-schema onboarding goldenset to w.
// Emitted when `mdql` is invoked in a directory with no schema.yml —
// teaches type grammar, command surface, wiki link forms, and ends
// with a bootstrapping prompt so an agent can propose a schema for
// the current directory rather than auto-scaffolding a preset.
func PrintOnboarding(w io.Writer) error {
	_, err := io.WriteString(w, onboardingDoc)
	return err
}
