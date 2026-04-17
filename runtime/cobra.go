package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/notambourine/mdql/format"
	"github.com/notambourine/mdql/schema"
	"github.com/notambourine/mdql/search"
	"github.com/notambourine/mdql/store/md"
	"github.com/notambourine/mdql/wiki"
)

// globals holds the persistent-flag values for the root command.
// A single struct keeps the flag wiring in one place and avoids
// package-level state that tests would have to reset.
type globals struct {
	root   string
	format string
	quiet  bool
}

// storeOpener is a factory the CLI calls lazily so we only open bleve
// when a command actually needs it (e.g. `init` should not require a
// pre-existing index).
type storeOpener func(ctx context.Context) (*md.Store, *search.Index, func(), error)

// BuildRootCmd returns the mdql cobra command tree driven by schema.
// The returned tree covers:
//   - per-entity CRUD: add, list, show, update (unless append_only),
//     archive (unless !archivable)
//   - cross-entity: init, index rebuild, search, wiki backlinks/orphans/
//     dangling, tag list/add/remove/delete/count
//
// Output is agent-friendly by default: terse one-line flag
// descriptions, no Long help text, json-for-pipe format resolution.
func BuildRootCmd(s *schema.Schema) *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:           "mdql",
		Short:         "schema-driven markdown CRUD + FTS + wiki",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&g.root, "root", ".", "store root")
	root.PersistentFlags().StringVarP(&g.format, "format", "f", "", "output: table|json|csv|tsv")
	root.PersistentFlags().BoolVarP(&g.quiet, "quiet", "q", false, "IDs only")

	opener := func(ctx context.Context) (*md.Store, *search.Index, func(), error) {
		abs, err := filepath.Abs(g.root)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("resolve root: %w", err)
		}
		idx, err := search.Open(filepath.Join(abs, s.Store.RuntimeDir, "index"))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("open index: %w", err)
		}
		store, err := md.Open(abs, s, idx)
		if err != nil {
			_ = idx.Close()
			return nil, nil, nil, err
		}
		return store, idx, func() { _ = store.Close(); _ = idx.Close() }, nil
	}

	registerInit(root, s, g)
	registerIndex(root, opener)
	registerSearch(root, opener, g)
	registerWiki(root, opener, g)
	registerTag(root, opener, g)
	for name, entity := range s.Entities {
		registerEntity(root, name, entity, opener, g)
	}
	return root
}

// ─── per-entity CRUD ─────────────────────────────────────────────────

func registerEntity(root *cobra.Command, kind string, entity schema.Entity, opener storeOpener, g *globals) {
	parent := &cobra.Command{Use: kind, Short: "manage " + kind}
	cols := defaultColumns(entity)

	parent.AddCommand(entityAddCmd(kind, entity, cols, opener, g))
	parent.AddCommand(entityListCmd(kind, cols, opener, g))
	parent.AddCommand(entityShowCmd(kind, cols, opener, g))
	if !entity.AppendOnly {
		parent.AddCommand(entityUpdateCmd(kind, entity, cols, opener, g))
	}
	if entity.IsArchivable() {
		parent.AddCommand(entityArchiveCmd(kind, opener))
	}
	root.AddCommand(parent)
}

func entityAddCmd(kind string, entity schema.Entity, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "create " + kind,
		Args:  cobra.NoArgs,
	}
	fb := registerFieldFlags(cmd, entity, true)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		store, _, closer, err := opener(cmd.Context())
		if err != nil {
			return err
		}
		defer closer()
		rec, err := store.Create(cmd.Context(), kind, fb.collectAll(cmd))
		if err != nil {
			return err
		}
		return renderRecords(g, cols, []map[string]any{rec})
	}
	return cmd
}

func entityListCmd(kind string, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	var tag string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list " + kind,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			filters := map[string]any{}
			if tag != "" {
				filters["tag"] = tag
			}
			recs, err := store.List(cmd.Context(), kind, filters)
			if err != nil {
				return err
			}
			if limit > 0 && len(recs) > limit {
				recs = recs[:limit]
			}
			return renderRecords(g, cols, recs)
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "filter by tag")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results")
	return cmd
}

func entityShowCmd(kind string, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "show <slug>",
		Short: "show " + kind,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			rec, err := store.Get(cmd.Context(), kind, args[0])
			if err != nil {
				return err
			}
			return renderRecords(g, cols, []map[string]any{rec})
		},
	}
}

func entityUpdateCmd(kind string, entity schema.Entity, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "update " + kind,
		Args:  cobra.ExactArgs(1),
	}
	fb := registerFieldFlags(cmd, entity, false)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		store, _, closer, err := opener(cmd.Context())
		if err != nil {
			return err
		}
		defer closer()
		rec, err := store.Update(cmd.Context(), kind, args[0], fb.collect(cmd))
		if err != nil {
			return err
		}
		return renderRecords(g, cols, []map[string]any{rec})
	}
	return cmd
}

func entityArchiveCmd(kind string, opener storeOpener) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <slug>",
		Short: "archive " + kind,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			if err := store.ArchiveEntity(cmd.Context(), kind, args[0]); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "archived", kind, args[0])
			return nil
		},
	}
}

// ─── cross-entity ───────────────────────────────────────────────────

func registerInit(root *cobra.Command, s *schema.Schema, g *globals) {
	root.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "scaffold store directories from schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, err := filepath.Abs(g.root)
			if err != nil {
				return err
			}
			return md.Init(abs, s)
		},
	})
}

func registerIndex(root *cobra.Command, opener storeOpener) {
	index := &cobra.Command{Use: "index", Short: "manage search index"}
	index.AddCommand(&cobra.Command{
		Use:   "rebuild",
		Short: "rebuild search index from disk",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			stats, err := store.Reindex(cmd.Context())
			if err != nil {
				return err
			}
			// Sort by kind so output is stable — map iteration in Go
			// is randomized, and agents diff this output in goldens.
			kinds := make([]string, 0, len(stats.ByKind))
			for kind := range stats.ByKind {
				kinds = append(kinds, kind)
			}
			sort.Strings(kinds)
			for _, kind := range kinds {
				fmt.Fprintf(os.Stdout, "%s\t%d\n", kind, stats.ByKind[kind])
			}
			return nil
		},
	})
	root.AddCommand(index)
}

func registerSearch(root *cobra.Command, opener storeOpener, g *globals) {
	var typeFilter string
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "full-text search",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, idx, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			results, err := idx.Search(args[0], typeFilter, limit)
			if err != nil {
				return err
			}
			return renderAny(g, results)
		},
	}
	cmd.Flags().StringVar(&typeFilter, "type", "", "filter by entity kind")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results")
	root.AddCommand(cmd)
}

func registerWiki(root *cobra.Command, opener storeOpener, g *globals) {
	parent := &cobra.Command{Use: "wiki", Short: "wiki graph queries"}

	parent.AddCommand(&cobra.Command{
		Use:   "backlinks <slug>",
		Short: "entities referencing [[slug]]",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, idx, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			refs, err := wiki.Backlinks(idx, args[0])
			if err != nil {
				return err
			}
			return renderAny(g, refs)
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:   "orphans",
		Short: "entities with no inbound links",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, idx, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			refs, err := wiki.Orphans(cmd.Context(), store, idx)
			if err != nil {
				return err
			}
			return renderAny(g, refs)
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:   "dangling",
		Short: "links whose target does not exist",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			refs, err := wiki.Check(cmd.Context(), store)
			if err != nil {
				return err
			}
			return renderAny(g, refs)
		},
	})
	root.AddCommand(parent)
}

func registerTag(root *cobra.Command, opener storeOpener, g *globals) {
	parent := &cobra.Command{Use: "tag", Short: "manage tags"}

	parent.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "list distinct tags with counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			counts, err := store.ListTags(cmd.Context())
			if err != nil {
				return err
			}
			return renderAny(g, counts)
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:   "add <kind> <slug> <tag>",
		Short: "tag an entity",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			return store.AddTag(cmd.Context(), args[0], args[1], args[2])
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:   "remove <kind> <slug> <tag>",
		Short: "untag an entity",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			return store.RemoveTag(cmd.Context(), args[0], args[1], args[2])
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:   "delete <tag>",
		Short: "remove a tag from every entity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			n, err := store.DeleteTag(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "modified %d entities\n", n)
			return nil
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:   "count <tag>",
		Short: "count entities using a tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			n, err := store.CountTagUsage(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, n)
			return nil
		},
	})
	root.AddCommand(parent)
}

// ─── rendering helpers ──────────────────────────────────────────────

func renderRecords(g *globals, cols []format.ColumnDef, recs []map[string]any) error {
	return format.Output(os.Stdout, format.Resolve(g.format), recs, cols, g.quiet)
}

// renderAny serializes arbitrary (non-map) result types. Reuses
// format.Output by round-tripping through JSON when the caller wants
// table/csv/tsv output; for json and quiet it's emitted directly to
// avoid a needless re-encode.
func renderAny(g *globals, v any) error {
	f := format.Resolve(g.format)
	if g.quiet {
		return writeQuietAny(os.Stdout, v)
	}
	if f == format.FormatJSON {
		return format.OutputJSONAny(os.Stdout, v)
	}
	recs, cols, err := anyToRecords(v)
	if err != nil {
		return err
	}
	return format.Output(os.Stdout, f, recs, cols, false)
}

// writeQuietAny emits the IDs-only view for structured results that
// have an ID field. Falls back to empty output if the shape is exotic.
func writeQuietAny(w io.Writer, v any) error {
	recs, _, err := anyToRecords(v)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if id, ok := r["id"]; ok {
			fmt.Fprintln(w, id)
		}
	}
	return nil
}
