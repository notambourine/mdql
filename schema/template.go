package schema

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

// funcMap lists helpers available inside title/slug templates. Keep
// this small — schema templates should be declarative, not a scripting
// surface.
var funcMap = template.FuncMap{
	"date": dateFormat,
}

// dateFormat accepts a Go reference-time layout plus a value that is
// either a time.Time or a parseable string, and returns the formatted
// date. Unknown inputs render as the empty string rather than panic,
// so broken frontmatter produces a skipped slug segment rather than a
// template execution error deep in cobra.
func dateFormat(layout string, v any) string {
	t, ok := coerceTime(v)
	if !ok {
		return ""
	}
	return t.Format(layout)
}

func coerceTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, x); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// Render parses and executes tmpl with data. It is called often (every
// title/slug render), so the caller may want to cache compiled
// templates; v1 keeps it simple and re-parses on each call.
func Render(tmpl string, data map[string]any) (string, error) {
	t, err := template.New("mdql").Funcs(funcMap).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("schema: parse template %q: %w", tmpl, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("schema: execute template %q: %w", tmpl, err)
	}
	return buf.String(), nil
}
