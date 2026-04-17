// Package main is the mdql CLI entry point.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "mdql: pre-release; engine not yet wired")
	os.Exit(1)
}
