package md

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/notambourine/mdql/schema"
)

// EntityVisitor is the callback invoked by ForEachEntity per record.
// slug is the canonical id (folder name for sprawl, filename sans ".md"
// for flat). path is the absolute path to the canonical frontmatter
// document (`index.md` for sprawl, `{slug}.md` for flat).
type EntityVisitor func(slug, path string, front, body []byte) error

// ForEachEntity invokes visit once per record of kind in sorted slug
// order. Sprawl entities are enumerated by reading immediate subfolders
// of the entity dir and loading each `index.md`; flat entities keep the
// legacy .md-file iteration. A missing entity dir is treated as empty.
//
// Exposed so callers outside the md package (wiki, lint) can iterate
// records without reimplementing sprawl vs flat path resolution.
func (s *Store) ForEachEntity(kind string, visit EntityVisitor) error {
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return fmt.Errorf("unknown entity kind %q", kind)
	}
	dir := filepath.Join(s.root, entity.Dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dir %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		slug, path, ok := entityCandidate(entity, dir, entry)
		if !ok {
			continue
		}
		front, body, err := Parse(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if err := visit(slug, path, front, body); err != nil {
			return err
		}
	}
	return nil
}

// entityCandidate maps a directory entry to (slug, canonical-path, ok).
// Sprawl: a subfolder with an index.md. Flat: any *.md file.
func entityCandidate(entity schema.Entity, dir string, entry os.DirEntry) (string, string, bool) {
	if entity.IsSprawl() {
		if !entry.IsDir() {
			return "", "", false
		}
		return entry.Name(), filepath.Join(dir, entry.Name(), "index.md"), true
	}
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
		return "", "", false
	}
	name := entry.Name()
	return name[:len(name)-len(".md")], filepath.Join(dir, name), true
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
