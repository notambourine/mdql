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
	noSync bool
	// schema is bound so `--schema` shows up in `mdql --help`. The
	// value isn't read here — the binary's main() pre-parses --schema
	// from os.Args to build this very command tree. Registering it on
	// cobra is purely cosmetic (advertise the flag in help output).
	schema string
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
	root.PersistentFlags().StringVar(&g.schema, "schema", "./schema.yml", "schema file path")
	root.PersistentFlags().BoolVar(&g.noSync, "no-sync", false, "skip stale-index check on open")

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
		if !g.noSync {
			if _, err := store.SyncIfStale(ctx); err != nil {
				_ = idx.Close()
				return nil, nil, nil, fmt.Errorf("sync index: %w", err)
			}
		}
		return store, idx, func() { _ = store.Close(); _ = idx.Close() }, nil
	}

	registerInit(root, s, g)
	registerIndex(root, opener)
	registerSearch(root, opener, g)
	registerWiki(root, opener, g)
	registerTag(root, opener, g)
	registerLint(root, opener, g)
	registerSchemaDescribe(root, s)
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
	parent.AddCommand(entityListCmd(kind, entity, cols, opener, g))
	parent.AddCommand(entityShowCmd(kind, entity, cols, opener, g))
	if !entity.AppendOnly {
		parent.AddCommand(entityUpdateCmd(kind, entity, cols, opener, g))
	}
	if entity.IsArchivable() {
		parent.AddCommand(entityArchiveCmd(kind, opener, g))
	}
	registerSubFiles(parent, kind, entity, opener, g)
	root.AddCommand(parent)
}

func entityAddCmd(kind string, entity schema.Entity, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "create " + kind,
		Args:  cobra.NoArgs,
	}
	fb := registerFieldFlags(cmd, entity, true)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without writing")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		store, _, closer, err := opener(cmd.Context())
		if err != nil {
			return err
		}
		defer closer()
		input := fb.collectAll(cmd)
		if dryRun {
			plan, err := store.PlanCreate(cmd.Context(), kind, input)
			if err != nil {
				return err
			}
			return renderPlan(g, plan)
		}
		rec, err := store.Create(cmd.Context(), kind, input)
		if err != nil {
			return err
		}
		return renderRecords(g, cols, []map[string]any{rec})
	}
	return cmd
}

func entityListCmd(kind string, entity schema.Entity, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	var tag string
	var limit int
	var fields []string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list " + kind,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectCols, err := resolveFieldsProjection(entity.Fields, cols, fields)
			if err != nil {
				return err
			}
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
			if len(fields) > 0 {
				recs = projectRecords(recs, fields)
			}
			return renderRecords(g, projectCols, recs)
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "filter by tag")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results")
	cmd.Flags().StringSliceVar(&fields, "fields", nil, "project to named fields")
	return cmd
}

func entityShowCmd(kind string, entity schema.Entity, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
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
			if entity.IsSprawl() && len(entity.Files) > 0 {
				subs, err := store.ListSubFileGraph(cmd.Context(), kind, args[0])
				if err != nil {
					return err
				}
				rec["sub_files"] = subFileGraphView(store.Root(), subs)
			}
			return renderRecord(g, cols, rec)
		},
	}
}

// subFileGraphItem is the JSON shape for one sub-file embedded in an
// entity `show` payload. Path is root-relative so goldens and agents
// get stable values across store locations. Body is intentionally
// omitted — the graph is a directory, not the full documents; drill
// down via `<entity> <subkind> show` for body content.
type subFileGraphItem struct {
	Kind   string         `json:"kind"`
	Slug   string         `json:"slug"`
	Path   string         `json:"path"`
	Fields map[string]any `json:"fields"`
}

func subFileGraphView(root string, recs []md.SubFileRecord) []subFileGraphItem {
	out := make([]subFileGraphItem, 0, len(recs))
	for _, r := range recs {
		rel, err := filepath.Rel(root, r.Path)
		if err != nil {
			rel = r.Path
		}
		out = append(out, subFileGraphItem{
			Kind:   r.Kind,
			Slug:   r.Slug,
			Path:   rel,
			Fields: r.Fields,
		})
	}
	return out
}

func entityUpdateCmd(kind string, entity schema.Entity, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "update " + kind,
		Args:  cobra.ExactArgs(1),
	}
	fb := registerFieldFlags(cmd, entity, false)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without writing")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		store, _, closer, err := opener(cmd.Context())
		if err != nil {
			return err
		}
		defer closer()
		patch := fb.collect(cmd)
		if dryRun {
			plan, err := store.PlanUpdate(cmd.Context(), kind, args[0], patch)
			if err != nil {
				return err
			}
			return renderPlan(g, plan)
		}
		rec, err := store.Update(cmd.Context(), kind, args[0], patch)
		if err != nil {
			return err
		}
		return renderRecords(g, cols, []map[string]any{rec})
	}
	return cmd
}

func entityArchiveCmd(kind string, opener storeOpener, g *globals) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "archive <slug>",
		Short: "archive " + kind,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			if dryRun {
				plan, err := store.PlanArchiveEntity(cmd.Context(), kind, args[0])
				if err != nil {
					return err
				}
				return renderPlan(g, plan)
			}
			if err := store.ArchiveEntity(cmd.Context(), kind, args[0]); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "archived", kind, args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without writing")
	return cmd
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
	var typeFilter, kindFilter, subFilter string
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
			filter := search.Filter{Type: typeFilter, ParentKind: kindFilter, SubKind: subFilter}
			results, err := idx.Search(args[0], filter, limit)
			if err != nil {
				return err
			}
			return renderAny(g, results)
		},
	}
	cmd.Flags().StringVar(&typeFilter, "type", "", "filter by entity kind or sub-file kind")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "filter by parent entity kind (for sub-files)")
	cmd.Flags().StringVar(&subFilter, "sub", "", "filter by sub-file kind")
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
		Short: "entities with no inbound [[wiki]] links",
		Long: "Reports entities that nothing else links to. " +
			"Outbound links from the entity do NOT count — an entity that " +
			"links to others but is itself unreferenced is still an orphan. " +
			"Useful for finding records that have fallen out of the graph.",
		Args: cobra.NoArgs,
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

// renderRecord is the singular sibling of renderRecords for commands
// that always return exactly one record (show). JSON emits a bare
// object so consumers don't have to .[0]; table/csv/tsv reuse the
// list renderer with a one-row slice.
func renderRecord(g *globals, cols []format.ColumnDef, rec map[string]any) error {
	if g.quiet {
		if id, ok := rec["id"]; ok {
			fmt.Fprintln(os.Stdout, id)
		}
		return nil
	}
	if format.Resolve(g.format) == format.FormatJSON {
		return format.OutputJSONAny(os.Stdout, rec)
	}
	return format.Output(os.Stdout, format.Resolve(g.format), []map[string]any{rec}, cols, false)
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

// renderPlan emits a WritePlan in the user-selected format. JSON is the
// agent-facing path (full plan shape); table/quiet fall back to a
// single terse line so human-driven runs don't drown in detail.
func renderPlan(g *globals, plan md.WritePlan) error {
	if g.quiet {
		fmt.Fprintln(os.Stdout, plan.Path)
		return nil
	}
	if format.Resolve(g.format) == format.FormatJSON {
		return format.OutputJSONAny(os.Stdout, plan)
	}
	if plan.ToPath != "" {
		fmt.Fprintf(os.Stdout, "action=%s kind=%s slug=%s path=%s to_path=%s\n", plan.Action, plan.Kind, plan.Slug, plan.Path, plan.ToPath)
	} else {
		fmt.Fprintf(os.Stdout, "action=%s kind=%s slug=%s path=%s\n", plan.Action, plan.Kind, plan.Slug, plan.Path)
	}
	return nil
}

// resolveFieldsProjection validates a --fields request against the
// entity/sub-file schema and returns the column layout the renderer
// should use. Falls through to the default cols when fields is empty.
//
// Validation runs before the store opens (bleve init is expensive) so
// an invalid --fields bails fast without touching the index.
func resolveFieldsProjection(fieldDefs map[string]schema.Field, defaultCols []format.ColumnDef, fields []string) ([]format.ColumnDef, error) {
	if len(fields) == 0 {
		return defaultCols, nil
	}
	for _, f := range fields {
		if f == "id" || f == "body" {
			continue
		}
		if _, ok := fieldDefs[f]; !ok {
			return nil, fmt.Errorf("unknown field: %s", f)
		}
	}
	cols := make([]format.ColumnDef, 0, len(fields))
	for _, f := range fields {
		cols = append(cols, format.ColumnDef{Header: humanize(f), Field: f})
	}
	return cols, nil
}

// projectRecords rebuilds each row with only the requested keys so JSON
// output matches the column-subset promise. Missing keys serialize as
// null, which is more explicit than silently dropping the column.
func projectRecords(recs []map[string]any, fields []string) []map[string]any {
	out := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		row := make(map[string]any, len(fields))
		for _, f := range fields {
			row[f] = r[f]
		}
		out = append(out, row)
	}
	return out
}
