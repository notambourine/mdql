package md

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/notambourine/mdql/model"
)

// nowRFC3339 returns the current UTC time formatted as RFC3339.
// A package variable so tests can override for deterministic timestamps.
var nowRFC3339 = func() string { return time.Now().UTC().Format(time.RFC3339) }

// walkEntities iterates the .md files in dir, calling visit for each.
// A missing directory is treated as empty (no error). Non-.md files and
// subdirectories are skipped.
func walkEntities(dir string, visit func(path string, front, body []byte) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dir %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		front, body, err := Parse(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if err := visit(path, front, body); err != nil {
			return err
		}
	}
	return nil
}

// decodeInto YAML-decodes front into v. Zero-length input is a no-op so
// files missing a frontmatter block decode to the zero value.
func decodeInto(front []byte, v any) error {
	if len(front) == 0 {
		return nil
	}
	if err := yaml.Unmarshal(front, v); err != nil {
		return fmt.Errorf("decode frontmatter: %w", err)
	}
	return nil
}

// decodeMap YAML-decodes front into a generic map. The generic entity
// layer uses this to avoid per-kind struct types.
func decodeMap(front []byte) (map[string]any, error) {
	out := map[string]any{}
	if len(front) == 0 {
		return out, nil
	}
	if err := yaml.Unmarshal(front, &out); err != nil {
		return nil, fmt.Errorf("decode frontmatter: %w", err)
	}
	return out, nil
}

// encodeFrontmatter YAML-encodes v for writing.
func encodeFrontmatter(v any) ([]byte, error) {
	buf, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	return buf, nil
}

// writeEntity encodes v's frontmatter, merging body, and atomically writes path.
func writeEntity(path string, v any, body string) error {
	front, err := encodeFrontmatter(v)
	if err != nil {
		return err
	}
	return Write(path, front, []byte(body))
}

// readEntity parses path into v and returns the body.
// Missing files are surfaced as model.ErrNotFound so callers can classify
// the error uniformly.
func readEntity(path string, v any) (body string, err error) {
	front, bodyBytes, err := Parse(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%s: %w", filepath.Base(path), model.ErrNotFound)
		}
		return "", err
	}
	if err := decodeInto(front, v); err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

// sortedDedup returns a sorted copy of xs with duplicates removed.
// Empty strings are dropped. Used to keep frontmatter arrays stable for
// git merges — same input produces the same output regardless of order.
func sortedDedup(xs []string) []string {
	if len(xs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(xs))
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		if _, dup := seen[x]; dup {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// ctxErr is a small helper so CRUD methods can check cancellation at
// entry without repeating three lines.
func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
