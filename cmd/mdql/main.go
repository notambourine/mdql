// Package main is the mdql CLI entry point.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/notambourine/mdql/engine"
	"github.com/notambourine/mdql/schema"
)

// Build info — set by goreleaser via -ldflags="-X main.version=... -X main.commit=... -X main.date=...".
// Defaults make `go install`-built binaries self-identify as dev rather than pretending to be a release.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// --version short-circuits schema load so `mdql --version` works
	// in any cwd, including fresh checkouts with no schema.yml.
	if hasFlag(os.Args, "--version") || hasFlag(os.Args, "-v") {
		fmt.Printf("mdql %s (%s, %s)\n", version, commit, date)
		os.Exit(0)
	}
	path := resolveSchemaPath(os.Args)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !explicitSchemaFlag(os.Args) {
			// No schema in cwd: emit the onboarding goldenset instead of
			// erroring. Teaches an agent (or human) the full mdql surface
			// and ends with a bootstrapping prompt.
			if err := engine.PrintOnboarding(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "mdql:", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "mdql:", err)
		os.Exit(1)
	}
	s, err := schema.Parse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdql:", err)
		os.Exit(1)
	}
	os.Exit(engine.Run(os.Args, s, engine.BuildInfo{Version: version, Commit: commit, Date: date}))
}

func explicitSchemaFlag(args []string) bool {
	for _, a := range args {
		if a == "--schema" || (len(a) >= len("--schema=") && a[:len("--schema=")] == "--schema=") {
			return true
		}
	}
	return false
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// resolveSchemaPath returns the value of --schema, defaulting to
// ./schema.yml. Must run before cobra sees argv because the schema
// shapes the command tree itself. Cobra re-parses the same flag to
// advertise it in `mdql --help`; the re-parsed value is unused.
func resolveSchemaPath(args []string) string {
	for i, a := range args {
		if a == "--schema" && i+1 < len(args) {
			return args[i+1]
		}
		if len(a) > len("--schema=") && a[:len("--schema=")] == "--schema=" {
			return a[len("--schema="):]
		}
	}
	return "./schema.yml"
}
