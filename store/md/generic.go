package md

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/notambourine/mdql/model"
	"github.com/notambourine/mdql/schema"
	"github.com/notambourine/mdql/store"
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
	p, err := s.prepareCreate(ctx, kind, input)
	if err != nil {
		return nil, err
	}
	if err := writeEntity(p.path, frontmatterOnly(p.rec), p.body); err != nil {
		return nil, err
	}
	if err := s.indexer.Upsert(p.doc); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	p.rec[bodyKey] = p.body
	return p.rec, nil
}

// PlanCreate runs the same validation and path-resolution as Create but
// touches neither disk nor indexer. The returned plan is what Create
// would have written if invoked with the same input.
func (s *Store) PlanCreate(ctx context.Context, kind string, input map[string]any) (WritePlan, error) {
	p, err := s.prepareCreate(ctx, kind, input)
	if err != nil {
		return WritePlan{}, err
	}
	return WritePlan{
		Action:      "create",
		Kind:        kind,
		Slug:        asString(p.rec["id"]),
		Path:        s.relPath(p.path),
		Frontmatter: frontmatterOnly(p.rec),
		Body:        p.body,
	}, nil
}

// preparedCreate carries everything Create and PlanCreate share: the
// fully-validated record (with id), the body that would go to disk,
// the canonical path, and the indexer doc.
type preparedCreate struct {
	entity schema.Entity
	rec    map[string]any
	body   string
	path   string
	doc    store.Doc
}

func (s *Store) prepareCreate(ctx context.Context, kind string, input map[string]any) (preparedCreate, error) {
	if err := ctxErr(ctx); err != nil {
		return preparedCreate{}, err
	}
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return preparedCreate{}, fmt.Errorf("unknown entity kind %q", kind)
	}
	rec := cloneMap(input)

	applyDefaults(entity, rec)
	if err := validateRequired(entity, rec); err != nil {
		return preparedCreate{}, err
	}
	if err := validateEnums(entity, rec); err != nil {
		return preparedCreate{}, err
	}
	normalizeTags(entity, rec)
	if err := s.checkUnique(ctx, kind, entity, rec, ""); err != nil {
		return preparedCreate{}, err
	}

	body, _ := rec[bodyKey].(string)
	delete(rec, bodyKey)

	baseRendered, err := schema.Render(entity.Slug, rec)
	if err != nil {
		return preparedCreate{}, fmt.Errorf("render slug: %w", err)
	}
	baseSlug := Slugify(baseRendered)
	if baseSlug == "" {
		return preparedCreate{}, fmt.Errorf("empty slug from %q: %w", entity.Slug, model.ErrValidation)
	}
	dir, err := s.EntityDir(kind)
	if err != nil {
		return preparedCreate{}, err
	}
	slug, err := ensureUniqueSlug(dir, baseSlug, entity.IsSprawl())
	if err != nil {
		return preparedCreate{}, err
	}
	rec["id"] = slug

	path, err := s.EntityPath(kind, slug)
	if err != nil {
		return preparedCreate{}, err
	}
	doc, err := entityDoc(kind, entity, rec, body)
	if err != nil {
		return preparedCreate{}, err
	}
	return preparedCreate{entity: entity, rec: rec, body: body, path: path, doc: doc}, nil
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
	return s.readMap(kind, slug, path)
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
	var out []map[string]any
	err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		rec, err := decodeMap(front)
		if err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		rec["id"] = slug
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
// body key is applied as the body.
func (s *Store) Update(ctx context.Context, kind, slug string, patch map[string]any) (map[string]any, error) {
	p, err := s.prepareUpdate(ctx, kind, slug, patch)
	if err != nil {
		return nil, err
	}
	if err := writeEntity(p.path, frontmatterOnly(p.rec), p.body); err != nil {
		return nil, err
	}
	if err := s.indexer.Upsert(p.doc); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	p.rec[bodyKey] = p.body
	return p.rec, nil
}

// PlanUpdate describes what Update would change without touching disk
// or the indexer. Includes a "before" snapshot so diffs are visible in
// one payload.
func (s *Store) PlanUpdate(ctx context.Context, kind, slug string, patch map[string]any) (WritePlan, error) {
	p, err := s.prepareUpdate(ctx, kind, slug, patch)
	if err != nil {
		return WritePlan{}, err
	}
	return WritePlan{
		Action:      "update",
		Kind:        kind,
		Slug:        slug,
		Path:        s.relPath(p.path),
		Frontmatter: frontmatterOnly(p.rec),
		Body:        p.body,
		Before:      &PlanBefore{Frontmatter: p.beforeFront, Body: p.beforeBody},
	}, nil
}

type preparedUpdate struct {
	entity      schema.Entity
	rec         map[string]any
	body        string
	path        string
	doc         store.Doc
	beforeFront map[string]any
	beforeBody  string
}

func (s *Store) prepareUpdate(ctx context.Context, kind, slug string, patch map[string]any) (preparedUpdate, error) {
	if err := ctxErr(ctx); err != nil {
		return preparedUpdate{}, err
	}
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return preparedUpdate{}, fmt.Errorf("unknown entity kind %q", kind)
	}
	path, err := s.EntityPath(kind, slug)
	if err != nil {
		return preparedUpdate{}, err
	}
	rec, err := s.readMap(kind, slug, path)
	if err != nil {
		return preparedUpdate{}, err
	}
	beforeBody, _ := rec[bodyKey].(string)
	beforeFront := frontmatterOnly(rec)

	for k, v := range patch {
		if v == nil {
			delete(rec, k)
			continue
		}
		rec[k] = v
	}
	if err := validateEnums(entity, rec); err != nil {
		return preparedUpdate{}, err
	}
	normalizeTags(entity, rec)
	if err := s.checkUnique(ctx, kind, entity, rec, slug); err != nil {
		return preparedUpdate{}, err
	}

	body, _ := rec[bodyKey].(string)
	delete(rec, bodyKey)

	doc, err := entityDoc(kind, entity, rec, body)
	if err != nil {
		return preparedUpdate{}, err
	}
	return preparedUpdate{
		entity:      entity,
		rec:         rec,
		body:        body,
		path:        path,
		doc:         doc,
		beforeFront: beforeFront,
		beforeBody:  beforeBody,
	}, nil
}

// ArchiveEntity soft-deletes the entity by moving its file under the
// archive tree and removing it from the index. Distinct from Archive
// (defined in archive.go) which is file-only; this is the method the
// runtime CLI should call.
//
// Sub-file docs owned by the archived entity are removed from the
// index first — otherwise they'd keep appearing in search results
// pointing at a path that no longer exists on disk.
func (s *Store) ArchiveEntity(ctx context.Context, kind, slug string) error {
	p, err := s.prepareArchive(ctx, kind, slug)
	if err != nil {
		return err
	}
	for _, subID := range p.subDocIDs {
		if err := s.indexer.Remove(subID); err != nil {
			return fmt.Errorf("deindex sub-file: %w", err)
		}
	}
	if err := s.Archive(kind, slug); err != nil {
		return err
	}
	return s.indexer.Remove(kind + ":" + slug)
}

// PlanArchiveEntity describes what ArchiveEntity would do without
// moving files or touching the indexer. The before block carries the
// current entity frontmatter and body; to_path is the archive
// destination.
func (s *Store) PlanArchiveEntity(ctx context.Context, kind, slug string) (WritePlan, error) {
	p, err := s.prepareArchive(ctx, kind, slug)
	if err != nil {
		return WritePlan{}, err
	}
	return WritePlan{
		Action: "archive",
		Kind:   kind,
		Slug:   slug,
		Path:   s.relPath(p.livePath),
		ToPath: s.relPath(p.archivePath),
		Before: &PlanBefore{Frontmatter: p.beforeFront, Body: p.beforeBody},
	}, nil
}

type preparedArchive struct {
	livePath    string
	archivePath string
	subDocIDs   []string
	beforeFront map[string]any
	beforeBody  string
}

func (s *Store) prepareArchive(ctx context.Context, kind, slug string) (preparedArchive, error) {
	if err := ctxErr(ctx); err != nil {
		return preparedArchive{}, err
	}
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return preparedArchive{}, fmt.Errorf("unknown entity kind %q", kind)
	}
	live, archived, err := s.archiveEndpoints(kind, slug)
	if err != nil {
		return preparedArchive{}, err
	}
	path, err := s.EntityPath(kind, slug)
	if err != nil {
		return preparedArchive{}, err
	}
	rec, err := s.readMap(kind, slug, path)
	if err != nil {
		return preparedArchive{}, err
	}
	beforeBody, _ := rec[bodyKey].(string)
	beforeFront := frontmatterOnly(rec)

	var subIDs []string
	if entity.IsSprawl() && len(entity.Files) > 0 {
		subs, err := s.ListSubFileGraph(ctx, kind, slug)
		if err != nil {
			return preparedArchive{}, err
		}
		for _, sub := range subs {
			subIDs = append(subIDs, subFileDocID(kind, slug, sub.Kind, sub.Slug))
		}
	}
	return preparedArchive{
		livePath:    live,
		archivePath: archived,
		subDocIDs:   subIDs,
		beforeFront: beforeFront,
		beforeBody:  beforeBody,
	}, nil
}

// relPath makes an absolute store path root-relative for display.
// Falls back to the absolute path when that fails (unreachable in
// practice — every caller hands us paths already rooted at s.root).
func (s *Store) relPath(abs string) string {
	rel, err := filepath.Rel(s.root, abs)
	if err != nil {
		return abs
	}
	return rel
}

// readMap loads the frontmatter + body at path into a generic map and
// sets rec["id"] = slug. Callers that know the slug (Get, Update) pass
// it in directly so the map-level id is authoritative regardless of
// sprawl vs flat layout.
func (s *Store) readMap(kind, slug, path string) (map[string]any, error) {
	front, body, err := Parse(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s/%s: %w", kind, slug, model.ErrNotFound)
		}
		return nil, err
	}
	rec, err := decodeMap(front)
	if err != nil {
		return nil, err
	}
	rec["id"] = slug
	rec[bodyKey] = string(body)
	return rec, nil
}

// KEY-DECISION 2026-04-17: stripping bodyKey here (not just id) lets
// the before-snapshot from a fresh readMap reuse this helper cleanly.
// Write paths already delete bodyKey before calling in, so the extra
// strip is a no-op there; dry-run Before blocks get clean frontmatter.
// frontmatterOnly returns a copy of rec with reserved meta keys
// stripped. Filename is the canonical id; body is rendered separately
// under the --- fence — neither belongs in the frontmatter block on
// disk or in the before-snapshot surfaced by a dry-run plan.
func frontmatterOnly(rec map[string]any) map[string]any {
	out := make(map[string]any, len(rec))
	for k, v := range rec {
		if k == "id" || k == bodyKey {
			continue
		}
		out[k] = v
	}
	return out
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
	return s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		if excludeSlug != "" && slug == excludeSlug {
			return nil
		}
		existing, err := decodeMap(front)
		if err != nil {
			return err
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

func ensureUniqueSlug(dir, base string, sprawl bool) (string, error) {
	if sprawl {
		return EnsureUniqueFolder(dir, base)
	}
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
