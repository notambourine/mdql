package runtime

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/notambourine/mdql/format"
	"github.com/notambourine/mdql/wiki"
)

// registerLint wires `mdql lint`. The command runs Lint, emits a report
// (JSON for agents, a one-line summary for --quiet, one finding per line
// otherwise), and exits with 0/1/2 based on the worst severity seen.
// Exit codes are distinct from the 2/3/4 runtime classifier so CI can
// wire lint into its own check without conflating with validation.
func registerLint(root *cobra.Command, opener storeOpener, g *globals) {
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "audit store for schema/disk drift",
		Long: "Walks every entity and sub-file under --root and reports " +
			"drift from schema. Exit codes: 0 clean/info-only, 1 warnings " +
			"present, 2 errors present.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			report, err := wiki.Lint(cmd.Context(), store)
			closer()
			if err != nil {
				return err
			}
			if err := emitLintReport(g, report); err != nil {
				return err
			}
			switch {
			case report.Summary.Error > 0:
				os.Exit(2)
			case report.Summary.Warn > 0:
				os.Exit(1)
			}
			return nil
		},
	}
	root.AddCommand(cmd)
}

// emitLintReport renders the report per --format/--quiet. JSON is a
// single marshaled payload; quiet is a one-liner for pipelines; the
// default human view prints one finding per line followed by a
// summary. Every line goes to stdout so scripts can redirect cleanly.
func emitLintReport(g *globals, report *wiki.Report) error {
	if g.quiet {
		fmt.Fprintf(os.Stdout, "errors=%d warns=%d info=%d\n",
			report.Summary.Error, report.Summary.Warn, report.Summary.Info)
		return nil
	}
	if format.Resolve(g.format) == format.FormatJSON {
		return format.OutputJSONAny(os.Stdout, report)
	}
	for _, f := range report.Findings {
		fmt.Fprintln(os.Stdout, formatFindingLine(f))
	}
	fmt.Fprintf(os.Stdout, "errors=%d warns=%d info=%d\n",
		report.Summary.Error, report.Summary.Warn, report.Summary.Info)
	return nil
}

// formatFindingLine renders one Finding as "SEVERITY code message [locator]".
// The locator is the root-relative path when present, falling back to the
// slug-tuple so dangling_link (no path) still carries its source identity.
func formatFindingLine(f wiki.Finding) string {
	locator := f.Path
	if locator == "" {
		parts := []string{}
		if f.Kind != "" {
			parts = append(parts, f.Kind)
		}
		if f.Slug != "" {
			parts = append(parts, f.Slug)
		}
		if f.SubKind != "" {
			parts = append(parts, f.SubKind)
		}
		if f.SubSlug != "" {
			parts = append(parts, f.SubSlug)
		}
		locator = strings.Join(parts, "/")
	}
	if locator != "" {
		return fmt.Sprintf("%-5s %s: %s [%s]", strings.ToUpper(f.Severity), f.Code, f.Message, locator)
	}
	return fmt.Sprintf("%-5s %s: %s", strings.ToUpper(f.Severity), f.Code, f.Message)
}
