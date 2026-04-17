package md

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/notambourine/mdql/model"
	"github.com/notambourine/mdql/schema"
)

// bodyKey is the conventional key in an input map that carries the
// markdown body. Anything else is frontmatter.
const bodyKey = "body"

// Create writes a new entity file and returns the stored record.
//
// Validation order: required → enum → defaults → unique. Slug is
// rendered from entity.Slug, slugified, and made collision-free via
// EnsureUnique. `body` is extracted from input before writing
// frontmatter.
func (s *Store) Create(ctx context.Context, kind string, input map[string]any) (map[string]any, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return nil, fmt.Errorf("unknown entity kind %q", kind)
	}
	rec := cloneMap(input)

	applyDefaults(entity, rec)
	if err := validateRequired(entity, rec); err != nil {
		return nil, err
	}
	if err := validateEnums(entity, rec); err != nil {
		return nil, err
	}
	normalizeTags(entity, rec)

	if err := s.checkUnique(ctx, kind, entity, rec, ""); err != nil {
		return nil, err
	}

	body, _ := rec[bodyKey].(string)
	delete(rec, bodyKey)

	baseRendered, err := schema.Render(entity.Slug, rec)
	if err != nil {
		return nil, fmt.Errorf("render slug: %w", err)
	}
	baseSlug := Slugify(baseRendered)
	if baseSlug == "" {
		return nil, fmt.Errorf("empty slug from %q: %w", entity.Slug, model.ErrValidation)
	}
	dir, err := s.EntityDir(kind)
	if err != nil {
		return nil, err
	}
	slug, err := ensureUniqueAtomic(dir, baseSlug)
	if err != nil {
		return nil, err
	}

	now := nowRFC3339()
	rec["id"] = slug
	if _, ok := rec["uuid"].(string); !ok {
		rec["uuid"] = uuid.New().String()
	}
	rec["created_at"] = now
	rec["updated_at"] = now

	path := filepath.Join(dir, slug+".md")
	if err := writeEntity(path, rec, body); err != nil {
		return nil, err
	}
	doc, err := entityDoc(kind, entity, rec, body)
	if err != nil {
		return nil, err
	}
	if err := s.indexer.Upsert(doc); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	rec[bodyKey] = body
	return rec, nil
}

// Get returns the entity with the given slug.
func (s *Store) Get(ctx context.Context, kind, slug string) (map[string]any, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	path, err := s.EntityPath(kind, slug)
	if err != nil {
		return nil, err
	}
	return s.readMap(path)
}

// List returns every entity of kind that matches filters, sorted by
// slug. Filter semantics:
//   - key "tag"  → entity's tags must contain the value
//   - any other  → entity's field must equal the value (case-sensitive)
//
// An empty/nil filters map returns everything.
func (s *Store) List(ctx context.Context, kind string, filters map[string]any) ([]map[string]any, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	dir, err := s.EntityDir(kind)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	err = walkEntities(dir, func(path string, front, body []byte) error {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		rec, err := decodeMap(front)
		if err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		rec[bodyKey] = string(body)
		if !matchFilters(rec, filters) {
			return nil
		}
		out = append(out, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return asString(out[i]["id"]) < asString(out[j]["id"])
	})
	return out, nil
}

// Update applies patch fields to the existing record. Keys whose value
// is nil are removed from the record; everything else overwrites. The
// body key is applied as the body. updated_at is bumped.
func (s *Store) Update(ctx context.Context, kind, slug string, patch map[string]any) (map[string]any, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return nil, fmt.Errorf("unknown entity kind %q", kind)
	}
	path, err := s.EntityPath(kind, slug)
	if err != nil {
		return nil, err
	}
	rec, err := s.readMap(path)
	if err != nil {
		return nil, err
	}

	for k, v := range patch {
		if v == nil {
			delete(rec, k)
			continue
		}
		rec[k] = v
	}
	if err := validateEnums(entity, rec); err != nil {
		return nil, err
	}
	normalizeTags(entity, rec)
	if err := s.checkUnique(ctx, kind, entity, rec, slug); err != nil {
		return nil, err
	}
	rec["updated_at"] = nowRFC3339()

	body, _ := rec[bodyKey].(string)
	delete(rec, bodyKey)

	if err := writeEntity(path, rec, body); err != nil {
		return nil, err
	}
	doc, err := entityDoc(kind, entity, rec, body)
	if err != nil {
		return nil, err
	}
	if err := s.indexer.Upsert(doc); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	rec[bodyKey] = body
	return rec, nil
}

// ArchiveEntity soft-deletes the entity by moving its file under the
// archive tree and removing it from the index. Distinct from Archive
// (defined in archive.go) which is file-only; this is the method the
// runtime CLI should call.
func (s *Store) ArchiveEntity(ctx context.Context, kind, slug string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := s.Archive(kind, slug); err != nil {
		return err
	}
	return s.indexer.Remove(kind + ":" + slug)
}

// readMap is the map[string]any version of readEntity.
func (s *Store) readMap(path string) (map[string]any, error) {
	front, body, err := Parse(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), model.ErrNotFound)
		}
		return nil, err
	}
	rec, err := decodeMap(front)
	if err != nil {
		return nil, err
	}
	rec[bodyKey] = string(body)
	return rec, nil
}

func (s *Store) checkUnique(ctx context.Context, kind string, entity schema.Entity, rec map[string]any, excludeSlug string) error {
	var uniqueFields []string
	for name, field := range entity.Fields {
		if field.Unique {
			uniqueFields = append(uniqueFields, name)
		}
	}
	if len(uniqueFields) == 0 {
		return nil
	}
	dir, err := s.EntityDir(kind)
	if err != nil {
		return err
	}
	return walkEntities(dir, func(path string, front, body []byte) error {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		existing, err := decodeMap(front)
		if err != nil {
			return err
		}
		if excludeSlug != "" && asString(existing["id"]) == excludeSlug {
			return nil
		}
		for _, fname := range uniqueFields {
			a := asString(rec[fname])
			b := asString(existing[fname])
			if a == "" || b == "" {
				continue
			}
			if a == b {
				return fmt.Errorf("%s with %s=%s already exists: %w", kind, fname, a, model.ErrConflict)
			}
		}
		return nil
	})
}

func validateRequired(entity schema.Entity, rec map[string]any) error {
	for name, field := range entity.Fields {
		if !field.Required {
			continue
		}
		if isBlank(rec[name]) {
			return fmt.Errorf("field %q is required: %w", name, model.ErrValidation)
		}
	}
	return nil
}

func validateEnums(entity schema.Entity, rec map[string]any) error {
	for name, field := range entity.Fields {
		if field.Type != "enum" {
			continue
		}
		v, ok := rec[name]
		if !ok || v == nil {
			continue
		}
		val := asString(v)
		if val == "" {
			continue
		}
		if !stringSliceContains(field.Values, val) {
			return fmt.Errorf("field %q=%q not in %v: %w", name, val, field.Values, model.ErrValidation)
		}
	}
	return nil
}

func applyDefaults(entity schema.Entity, rec map[string]any) {
	for name, field := range entity.Fields {
		if field.Default == nil {
			continue
		}
		if _, has := rec[name]; has && !isBlank(rec[name]) {
			continue
		}
		rec[name] = field.Default
	}
}

func normalizeTags(entity schema.Entity, rec map[string]any) {
	for name, field := range entity.Fields {
		if field.Type != "string[]" || !field.Sorted {
			continue
		}
		if v, ok := rec[name]; ok {
			rec[name] = sortedDedup(toStringSlice(v))
		}
	}
}

func matchFilters(rec map[string]any, filters map[string]any) bool {
	for k, v := range filters {
		if v == nil {
			continue
		}
		if k == "tag" {
			want := asString(v)
			if want == "" {
				continue
			}
			found := false
			for _, t := range toStringSlice(rec["tags"]) {
				if t == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
			continue
		}
		if asString(rec[k]) != asString(v) {
			return false
		}
	}
	return true
}

func ensureUniqueAtomic(dir, base string) (string, error) {
	return EnsureUnique(dir, base)
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isBlank(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) == ""
	case []string:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}
	return false
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func stringSliceContains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
