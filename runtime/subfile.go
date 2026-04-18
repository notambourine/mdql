package runtime

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/notambourine/mdql/format"
	"github.com/notambourine/mdql/schema"
	"github.com/notambourine/mdql/store/md"
)

// registerSubFiles wires one cobra subtree per declared sub-file kind
// under the parent entity command. The verb surface mirrors the entity
// CRUD surface (add/list/show/update/delete) so an agent that knows how
// to drive one kind knows how to drive the other — the only extra arg
// is the parent slug.
//
// Invoked from registerEntity so cross-cutting changes (new global
// flags, new rendering paths) land in one place.
func registerSubFiles(parent *cobra.Command, kind string, entity schema.Entity, opener storeOpener, g *globals) {
	if !entity.IsSprawl() || len(entity.Files) == 0 {
		return
	}
	names := make([]string, 0, len(entity.Files))
	for name := range entity.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, subName := range names {
		sub := entity.Files[subName]
		sfCmd := &cobra.Command{Use: subName, Short: "manage " + kind + "/" + subName}
		cols := subFileColumns(sub)

		sfCmd.AddCommand(subFileAddCmd(kind, subName, sub, cols, opener, g))
		sfCmd.AddCommand(subFileListCmd(kind, subName, sub, cols, opener, g))
		sfCmd.AddCommand(subFileShowCmd(kind, subName, cols, opener, g))
		sfCmd.AddCommand(subFileUpdateCmd(kind, subName, sub, cols, opener, g))
		sfCmd.AddCommand(subFileDeleteCmd(kind, subName, opener, g))
		parent.AddCommand(sfCmd)
	}
}

// subFileColumns picks the default column layout for a sub-file's
// table/csv rendering: id first, then each scalar schema field in
// alphabetical order. Arrays and long free-text fields are skipped —
// same rules as defaultColumns for entities.
func subFileColumns(sub schema.SubFile) []format.ColumnDef {
	cols := []format.ColumnDef{{Header: "ID", Field: "id"}}
	names := make([]string, 0, len(sub.Fields))
	for name := range sub.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		field := sub.Fields[name]
		if !isScalarForList(field.Type) {
			continue
		}
		cols = append(cols, format.ColumnDef{Header: humanize(name), Field: name})
		if len(cols) >= maxListColumns {
			break
		}
	}
	return cols
}

func subFileAddCmd(parentKind, subKind string, sub schema.SubFile, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "add <parent-slug>",
		Short: "create " + parentKind + "/" + subKind,
		Args:  cobra.ExactArgs(1),
	}
	fb := registerSubFieldFlags(cmd, sub, true)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without writing")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		store, _, closer, err := opener(cmd.Context())
		if err != nil {
			return err
		}
		defer closer()
		input := fb.collectAll(cmd)
		if dryRun {
			plan, err := store.PlanCreateSubFile(cmd.Context(), parentKind, args[0], subKind, input)
			if err != nil {
				return err
			}
			return renderPlan(g, plan)
		}
		rec, err := store.CreateSubFile(cmd.Context(), parentKind, args[0], subKind, input)
		if err != nil {
			return err
		}
		return renderRecords(g, cols, []map[string]any{subFileRow(rec)})
	}
	return cmd
}

func subFileListCmd(parentKind, subKind string, sub schema.SubFile, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	var fields []string
	cmd := &cobra.Command{
		Use:   "list <parent-slug>",
		Short: "list " + parentKind + "/" + subKind,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectCols, err := resolveFieldsProjection(sub.Fields, cols, fields)
			if err != nil {
				return err
			}
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			recs, err := store.ListSubFiles(cmd.Context(), parentKind, args[0], subKind)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(recs))
			for _, r := range recs {
				rows = append(rows, subFileRow(r))
			}
			if len(fields) > 0 {
				rows = projectRecords(rows, fields)
			}
			return renderRecords(g, projectCols, rows)
		},
	}
	cmd.Flags().StringSliceVar(&fields, "fields", nil, "project to named fields")
	return cmd
}

func subFileShowCmd(parentKind, subKind string, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "show <parent-slug> <sub-slug>",
		Short: "show " + parentKind + "/" + subKind,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			rec, err := store.GetSubFile(cmd.Context(), parentKind, args[0], subKind, args[1])
			if err != nil {
				return err
			}
			return renderRecord(g, cols, subFileRow(rec))
		},
	}
}

func subFileUpdateCmd(parentKind, subKind string, sub schema.SubFile, cols []format.ColumnDef, opener storeOpener, g *globals) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "update <parent-slug> <sub-slug>",
		Short: "update " + parentKind + "/" + subKind,
		Args:  cobra.ExactArgs(2),
	}
	fb := registerSubFieldFlags(cmd, sub, false)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without writing")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		store, _, closer, err := opener(cmd.Context())
		if err != nil {
			return err
		}
		defer closer()
		patch := fb.collect(cmd)
		if dryRun {
			plan, err := store.PlanUpdateSubFile(cmd.Context(), parentKind, args[0], subKind, args[1], patch)
			if err != nil {
				return err
			}
			return renderPlan(g, plan)
		}
		rec, err := store.UpdateSubFile(cmd.Context(), parentKind, args[0], subKind, args[1], patch)
		if err != nil {
			return err
		}
		return renderRecords(g, cols, []map[string]any{subFileRow(rec)})
	}
	return cmd
}

func subFileDeleteCmd(parentKind, subKind string, opener storeOpener, g *globals) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "delete <parent-slug> <sub-slug>",
		Short: "delete " + parentKind + "/" + subKind,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, closer, err := opener(cmd.Context())
			if err != nil {
				return err
			}
			defer closer()
			if dryRun {
				plan, err := store.PlanDeleteSubFile(cmd.Context(), parentKind, args[0], subKind, args[1])
				if err != nil {
					return err
				}
				return renderPlan(g, plan)
			}
			if err := store.DeleteSubFile(cmd.Context(), parentKind, args[0], subKind, args[1]); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "deleted", parentKind, args[0], subKind, args[1])
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without writing")
	return cmd
}

// subFileRow flattens a SubFileRecord into the map[string]any that the
// table/json renderer consumes. Keeps the renderer agnostic of whether
// the row came from an entity or a sub-file.
func subFileRow(r md.SubFileRecord) map[string]any {
	out := map[string]any{}
	for k, v := range r.Fields {
		out[k] = v
	}
	out["id"] = r.Slug
	return out
}

// registerSubFieldFlags is a sub-file-shaped copy of registerFieldFlags.
// Kept separate because schema.SubFile.Fields is its own map type; the
// flag wiring is otherwise identical.
func registerSubFieldFlags(cmd *cobra.Command, sub schema.SubFile, markRequired bool) *flagBindings {
	synthetic := schema.Entity{Fields: sub.Fields}
	return registerFieldFlags(cmd, synthetic, markRequired)
}
