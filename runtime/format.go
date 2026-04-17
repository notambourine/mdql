package runtime

import (
	"strings"

	"github.com/notambourine/mdql/format"
	"github.com/notambourine/mdql/schema"
)

// maxListColumns caps the default list view at a readable width. v1.1
// will add per-entity list_columns: to override.
const maxListColumns = 8

// defaultColumns derives table columns for an entity from its schema
// fields. Rules: always include id first, then every non-array scalar
// up to maxListColumns, in field-declaration order as produced by
// yaml.v3 (alphabetical for map[string]…). Arrays, relation[], and
// long free-text fields are skipped — they make tables unreadable.
func defaultColumns(entity schema.Entity) []format.ColumnDef {
	cols := []format.ColumnDef{{Header: "ID", Field: "id"}}
	names := sortedFieldNames(entity)
	for _, name := range names {
		field := entity.Fields[name]
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

func isScalarForList(t string) bool {
	switch t {
	case "string", "int", "float", "bool", "date", "enum", "link":
		return true
	}
	return false
}

// humanize turns "first_name" into "First Name" for table headers.
func humanize(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func sortedFieldNames(entity schema.Entity) []string {
	names := make([]string, 0, len(entity.Fields))
	for name := range entity.Fields {
		names = append(names, name)
	}
	// Sort for stability across runs; map iteration order is random.
	sortStrings(names)
	return names
}

// sortStrings sorts in-place without pulling sort into every caller.
func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
