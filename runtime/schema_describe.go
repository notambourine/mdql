package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/notambourine/mdql/schema"
)

// schemaDescription is the agent-loadable view of a parsed schema.
// Output is the canonical session-entry payload: an LLM that loads this
// can derive every valid mdql command without reading any other file.
type schemaDescription struct {
	Store          schema.StoreConfig            `json:"store"`
	Entities       map[string]entityDescription  `json:"entities"`
	GlobalCommands []string                      `json:"global_commands"`
}

type entityDescription struct {
	Dir        string `json:"dir"`
	Title      string `json:"title"`
	Slug       string `json:"slug"`
	Archivable bool   `json:"archivable"`
	AppendOnly bool   `json:"append_only"`
	// Sprawl reports the on-disk layout: true = `{dir}/{slug}/index.md`
	// with free-form sibling files, false = single `{dir}/{slug}.md`.
	// Sprawl is the default; a schema entity opts out with `flat: true`.
	Sprawl            bool                      `json:"sprawl"`
	FrontmatterFields map[string]fieldView      `json:"frontmatter_fields"`
	SubFiles          map[string]subFileView    `json:"sub_files"`
	Commands          []string                  `json:"commands"`
}

// subFileView is the agent-loadable description of one sub-file kind.
// Mirrors entityDescription's field-level shape so LLMs can reason
// about sub-files with the same mental model they use for entities.
type subFileView struct {
	Dir               string               `json:"dir"`
	Slug              string               `json:"slug,omitempty"`
	Title             string               `json:"title,omitempty"`
	Catchall          bool                 `json:"catchall,omitempty"`
	FrontmatterFields map[string]fieldView `json:"frontmatter_fields"`
	Commands          []string             `json:"commands"`
}

// fieldView mirrors schema.Field but with omitempty so the JSON stays
// terse — agents care about the present constraints, not zero values.
type fieldView struct {
	Type      string   `json:"type"`
	Required  bool     `json:"required,omitempty"`
	Unique    bool     `json:"unique,omitempty"`
	Sorted    bool     `json:"sorted,omitempty"`
	Default   any      `json:"default,omitempty"`
	Values    []string `json:"values,omitempty"`
	Target    string   `json:"target,omitempty"`
	EdgeTypes []string `json:"edge_types,omitempty"`
}

// describeSchema builds the structured view used by `mdql schema describe`.
func describeSchema(s *schema.Schema) schemaDescription {
	out := schemaDescription{
		Store:    s.Store,
		Entities: make(map[string]entityDescription, len(s.Entities)),
		GlobalCommands: []string{
			"init",
			"index rebuild",
			"search",
			"wiki backlinks",
			"wiki orphans",
			"wiki dangling",
			"tag list",
			"tag add",
			"tag remove",
			"tag delete",
			"tag count",
			"schema describe",
		},
	}
	for name, entity := range s.Entities {
		out.Entities[name] = entityDescription{
			Dir:               entity.Dir,
			Title:             entity.Title,
			Slug:              entity.Slug,
			Archivable:        entity.IsArchivable(),
			AppendOnly:        entity.AppendOnly,
			Sprawl:            entity.IsSprawl(),
			FrontmatterFields: convertFields(entity.Fields),
			SubFiles:          convertSubFiles(name, entity.Files),
			Commands:          entityCommands(entity),
		}
	}
	return out
}

// convertSubFiles produces the agent-loadable sub-file views. Empty
// when the entity has no files: block, so the JSON shape stays stable
// (callers can iterate `sub_files` without nil-checks).
func convertSubFiles(entityName string, in map[string]schema.SubFile) map[string]subFileView {
	out := make(map[string]subFileView, len(in))
	for name, sub := range in {
		out[name] = subFileView{
			Dir:               sub.Dir,
			Slug:              sub.Slug,
			Title:             sub.Title,
			Catchall:          sub.Catchall,
			FrontmatterFields: convertFields(sub.Fields),
			Commands:          subFileCommandList(entityName, name),
		}
	}
	return out
}

// subFileCommandList is the verb surface the CLI registers for a
// sub-file kind. Kept alphabetical (matches entityCommands).
func subFileCommandList(entityName, subName string) []string {
	verbs := []string{"add", "delete", "list", "show", "update"}
	out := make([]string, 0, len(verbs))
	for _, v := range verbs {
		out = append(out, entityName+" "+subName+" "+v)
	}
	return out
}

func convertFields(in map[string]schema.Field) map[string]fieldView {
	out := make(map[string]fieldView, len(in))
	for name, f := range in {
		out[name] = fieldView{
			Type:      f.Type,
			Required:  f.Required,
			Unique:    f.Unique,
			Sorted:    f.Sorted,
			Default:   f.Default,
			Values:    f.Values,
			Target:    f.Target,
			EdgeTypes: f.EdgeTypes,
		}
	}
	return out
}

func entityCommands(entity schema.Entity) []string {
	cmds := []string{"add", "list", "show"}
	if !entity.AppendOnly {
		cmds = append(cmds, "update")
	}
	if entity.IsArchivable() {
		cmds = append(cmds, "archive")
	}
	sort.Strings(cmds)
	return cmds
}

// registerSchemaDescribe wires `mdql schema describe` into the root.
// JSON is the only supported format — this is the agent-context entry
// point and structured output is the contract.
func registerSchemaDescribe(root *cobra.Command, s *schema.Schema) {
	parent := &cobra.Command{Use: "schema", Short: "schema introspection"}
	parent.AddCommand(&cobra.Command{
		Use:   "describe",
		Short: "structured view of entities, fields, and commands",
		Long: "Full entity + field + sub-file + command graph. Always JSON.\n" +
			"\n" +
			"Shape: .entities.<kind>.frontmatter_fields is a MAP keyed by field\n" +
			"name, not an array. Iterate with `| to_entries[]`; the common mistake\n" +
			"`| .[] | select(.name==…)` returns nothing because there is no .name key.\n" +
			"\n" +
			"Common recipes:\n" +
			"  # field types for a kind\n" +
			"  mdql schema describe --format json \\\n" +
			"    | jq '.entities.<kind>.frontmatter_fields | to_entries[] | {(.key): .value.type}'\n" +
			"\n" +
			"  # required fields for a kind\n" +
			"  mdql schema describe --format json \\\n" +
			"    | jq '.entities.<kind>.frontmatter_fields | to_entries[] | select(.value.required) | .key'",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			desc := describeSchema(s)
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(desc); err != nil {
				return fmt.Errorf("encode: %w", err)
			}
			return nil
		},
	})
	root.AddCommand(parent)
}
