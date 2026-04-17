package runtime

import (
	"encoding/json"
	"fmt"

	"github.com/notambourine/mdql/format"
)

// anyToRecords JSON-round-trips v into a slice of maps plus derived
// columns. Used when the caller asks for table/csv/tsv output on a
// typed slice (e.g. []search.Result, []wiki.Ref): json tags already
// give us the right column names and snake_case keys.
//
// Scalars and structs are wrapped in a single-row slice; nil is
// returned as an empty slice.
func anyToRecords(v any) ([]map[string]any, []format.ColumnDef, error) {
	if v == nil {
		return nil, nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal: %w", err)
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, columnsFromList(list), nil
	}
	var single map[string]any
	if err := json.Unmarshal(raw, &single); err == nil {
		list = []map[string]any{single}
		return list, columnsFromList(list), nil
	}
	return nil, nil, fmt.Errorf("unsupported result shape")
}

// columnsFromList derives headers from the first row's keys, preserving
// a stable alphabetical order. Table callers already sort columns in
// the schema-driven path; the non-schema path uses the same rule so
// output is deterministic across invocations.
func columnsFromList(list []map[string]any) []format.ColumnDef {
	if len(list) == 0 {
		return nil
	}
	keys := make([]string, 0, len(list[0]))
	for k := range list[0] {
		keys = append(keys, k)
	}
	sortStrings(keys)
	cols := make([]format.ColumnDef, 0, len(keys))
	for _, k := range keys {
		cols = append(cols, format.ColumnDef{Header: humanize(k), Field: k})
	}
	return cols
}
