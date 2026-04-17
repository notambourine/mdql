// Package main is the mdql CLI entry point.
package main

import (
	"fmt"
	"os"

	"github.com/notambourine/mdql/engine"
	"github.com/notambourine/mdql/schema"
)

func main() {
	path := resolveSchemaPath(os.Args)
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdql:", err)
		os.Exit(1)
	}
	s, err := schema.Parse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdql:", err)
		os.Exit(1)
	}
	os.Exit(engine.Run(os.Args, s))
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
