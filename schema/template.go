package schema

import (
	"bytes"
	"fmt"
	"strings"
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

// Render parses and executes tmpl with data. Missing map keys would
// normally render as `<no value>`; on a `map[string]any`, `missingkey=zero`
// still produces `<nil>` because the zero value of the interface type
// is nil. We strip both sentinels post-execute so optional fields
// render as empty strings rather than leaking `-no-value` / `-nil-`
// fragments into slugs and titles.
func Render(tmpl string, data map[string]any) (string, error) {
	t, err := template.New("mdql").Funcs(funcMap).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("schema: parse template %q: %w", tmpl, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("schema: execute template %q: %w", tmpl, err)
	}
	out := buf.String()
	out = strings.ReplaceAll(out, "<no value>", "")
	out = strings.ReplaceAll(out, "<nil>", "")
	return out, nil
}
