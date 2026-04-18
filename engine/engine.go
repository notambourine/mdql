// Package engine wires the parsed schema, runtime cobra tree, and
// signal handling into a single Run entry point. Callers (including
// the mdql binary and any app embedding mdql via go:embed) hand in
// their schema and argv and get back a Unix exit code.
package engine

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/notambourine/mdql/model"
	"github.com/notambourine/mdql/runtime"
	"github.com/notambourine/mdql/schema"
)

// BuildInfo carries ldflags-injected version metadata.
// Zero value is valid — callers (e.g. library embedders) can omit it.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Run builds the cobra tree from schema, dispatches args[1:], and
// translates errors into Unix exit codes via model.ExitCode.
//
// args follows os.Args convention: args[0] is the program name,
// consumed only for help text.
func Run(args []string, s *schema.Schema, info BuildInfo) int {
	if len(args) == 0 {
		args = []string{"mdql"}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	root := runtime.BuildRootCmd(s)
	if info.Version != "" {
		root.Version = fmt.Sprintf("%s (%s, %s)", info.Version, info.Commit, info.Date)
	}
	root.SetArgs(args[1:])
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "mdql:", err)
		return model.ExitCode(err)
	}
	return 0
}
