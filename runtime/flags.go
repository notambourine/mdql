// Package runtime turns a parsed schema into a cobra command tree.
//
// flags.go handles the type-directed mapping from schema field types
// onto cobra flag registrations, and provides the helper that collects
// only the flags the user actually set into a Create/Update input map.
package runtime

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/notambourine/mdql/schema"
)

// fieldFlag captures the pointer cobra will populate for one field.
// Exactly one of the value pointers is non-nil, chosen by field.Type.
type fieldFlag struct {
	name  string
	field schema.Field
	str   *string
	integer *int
	float *float64
	boolean *bool
	slice *[]string
}

// flagBindings holds the per-entity flag pointers plus the body
// pointer. After RunE, Collect() walks the map and returns only the
// fields the user actually touched.
type flagBindings struct {
	fields map[string]*fieldFlag
	body   *string
}

// registerFieldFlags adds one cobra flag per schema field to cmd.
// When required is true, flags derived from required fields are also
// marked required via MarkFlagRequired.
//
// relation[] fields are flagless — they are managed via the cross-entity
// `relate` command once that ships.
func registerFieldFlags(cmd *cobra.Command, entity schema.Entity, markRequired bool) *flagBindings {
	fb := &flagBindings{fields: map[string]*fieldFlag{}}
	body := ""
	fb.body = &body
	cmd.Flags().StringVar(fb.body, "body", "", "markdown body")

	for name, field := range entity.Fields {
		if field.Type == "relation[]" {
			continue
		}
		flagName := strings.ReplaceAll(name, "_", "-")
		desc := fieldFlagDesc(name, field)
		ff := &fieldFlag{name: name, field: field}
		switch field.Type {
		case "int":
			v := 0
			ff.integer = &v
			cmd.Flags().IntVar(ff.integer, flagName, 0, desc)
		case "float":
			v := 0.0
			ff.float = &v
			cmd.Flags().Float64Var(ff.float, flagName, 0, desc)
		case "bool":
			v := false
			ff.boolean = &v
			cmd.Flags().BoolVar(ff.boolean, flagName, false, desc)
		case "string[]", "link[]":
			v := []string{}
			ff.slice = &v
			cmd.Flags().StringSliceVar(ff.slice, flagName, nil, desc)
		default:
			// string, link, enum, date all land here.
			v := ""
			ff.str = &v
			cmd.Flags().StringVar(ff.str, flagName, "", desc)
		}
		fb.fields[flagName] = ff
		if markRequired && field.Required {
			_ = cmd.MarkFlagRequired(flagName)
		}
	}
	return fb
}

// fieldFlagDesc produces a terse one-line flag description. Keeps the
// --help footprint small (agents read this output). Returns empty
// string for plain scalars whose cobra-rendered type annotation
// (`--email string`) already conveys everything useful.
func fieldFlagDesc(name string, field schema.Field) string {
	switch field.Type {
	case "enum":
		return strings.Join(field.Values, "|")
	case "link":
		return field.Target + " slug"
	case "link[]":
		return field.Target + " slugs"
	case "date":
		return "RFC3339 or YYYY-MM-DD"
	}
	if field.Required {
		return "required"
	}
	return ""
}


// collect returns an input map containing only the flags the user set
// plus body when it was set. Used by Update so we don't overwrite
// fields that weren't touched.
func (fb *flagBindings) collect(cmd *cobra.Command) map[string]any {
	out := map[string]any{}
	for flagName, ff := range fb.fields {
		if !cmd.Flags().Changed(flagName) {
			continue
		}
		out[ff.name] = ff.value()
	}
	if cmd.Flags().Changed("body") {
		out["body"] = *fb.body
	}
	return out
}

// collectAll returns every flag's value regardless of whether it was
// changed. Used by Create — schema defaults fill in anything unset.
func (fb *flagBindings) collectAll(cmd *cobra.Command) map[string]any {
	out := map[string]any{}
	for flagName, ff := range fb.fields {
		if !cmd.Flags().Changed(flagName) {
			continue
		}
		out[ff.name] = ff.value()
	}
	if *fb.body != "" {
		out["body"] = *fb.body
	}
	return out
}

func (ff *fieldFlag) value() any {
	switch {
	case ff.str != nil:
		return *ff.str
	case ff.integer != nil:
		return *ff.integer
	case ff.float != nil:
		return *ff.float
	case ff.boolean != nil:
		return *ff.boolean
	case ff.slice != nil:
		return append([]string(nil), *ff.slice...)
	}
	return nil
}
