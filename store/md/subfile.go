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
)

// SubFileRecord is the in-memory form of a sub-file: frontmatter map +
// body + identity (kind, parent slug, sub-file slug). Returned by the
// sub-file CRUD methods in the same shape entity CRUD uses so the CLI
// renderer doesn't need a separate code path.
type SubFileRecord struct {
	Kind       string         `json:"kind"`
	ParentKind string         `json:"parent_kind"`
	ParentSlug string         `json:"parent_slug"`
	Slug       string         `json:"slug"`
	Path       string         `json:"path"`
	Fields     map[string]any `json:"fields"`
	Body       string         `json:"body,omitempty"`
}

// SubFilePath returns the absolute path of a sub-file record.
// `{root}/{entity.Dir}/{parentSlug}/{subfile.Dir}/{subSlug}.md`.
func (s *Store) SubFilePath(parentKind, parentSlug, subKind, subSlug string) (string, error) {
	entity, sub, err := s.resolveSubFile(parentKind, subKind)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, entity.Dir, parentSlug, sub.Dir, subSlug+".md"), nil
}

// SubFileDir returns the folder that holds every record of one sub-file
// kind under the given parent.
func (s *Store) SubFileDir(parentKind, parentSlug, subKind string) (string, error) {
	entity, sub, err := s.resolveSubFile(parentKind, subKind)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, entity.Dir, parentSlug, sub.Dir), nil
}

// resolveSubFile looks up the parent entity and the named sub-file kind,
// returning both. Errors if either is missing or the parent is flat
// (flat entities can't host sub-files — they have no folder).
func (s *Store) resolveSubFile(parentKind, subKind string) (schema.Entity, schema.SubFile, error) {
	entity, ok := s.schema.Entities[parentKind]
	if !ok {
		return schema.Entity{}, schema.SubFile{}, fmt.Errorf("unknown entity kind %q", parentKind)
	}
	if !entity.IsSprawl() {
		return schema.Entity{}, schema.SubFile{}, fmt.Errorf("entity %q is flat, no sub-files", parentKind)
	}
	sub, ok := entity.Files[subKind]
	if !ok {
		return schema.Entity{}, schema.SubFile{}, fmt.Errorf("entity %q has no sub-file kind %q", parentKind, subKind)
	}
	return entity, sub, nil
}

// SubFileVisitor is the callback for ForEachSubFile.
type SubFileVisitor func(rec SubFileRecord, front, body []byte) error

// ForEachSubFile iterates every declared sub-file of parentKind/parentSlug,
// optionally scoped to one sub-file kind. When subKindFilter is empty,
// every sub-file kind under the parent is visited in (kind, slug) order.
//
// Catchall kinds also absorb any untyped .md that landed directly under
// the entity folder (not under a declared sub-file dir).
func (s *Store) ForEachSubFile(parentKind, parentSlug, subKindFilter string, visit SubFileVisitor) error {
	entity, ok := s.schema.Entities[parentKind]
	if !ok {
		return fmt.Errorf("unknown entity kind %q", parentKind)
	}
	if !entity.IsSprawl() {
		return nil
	}
	parentDir := filepath.Join(s.root, entity.Dir, parentSlug)
	if _, err := os.Stat(parentDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", parentDir, err)
	}

	kinds := make([]string, 0, len(entity.Files))
	for name := range entity.Files {
		if subKindFilter != "" && name != subKindFilter {
			continue
		}
		kinds = append(kinds, name)
	}
	sort.Strings(kinds)

	for _, subKind := range kinds {
		sub := entity.Files[subKind]
		dir := filepath.Join(parentDir, sub.Dir)
		if err := iterateSubFileDir(parentKind, parentSlug, subKind, dir, visit); err != nil {
			return err
		}
		if sub.Catchall && subKindFilter == "" {
			if err := iterateCatchallLoose(parentKind, parentSlug, subKind, parentDir, entity, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func iterateSubFileDir(parentKind, parentSlug, subKind, dir string, visit SubFileVisitor) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(entry.Name(), ".md")
		path := filepath.Join(dir, entry.Name())
		if err := emitSubFile(parentKind, parentSlug, subKind, slug, path, visit); err != nil {
			return err
		}
	}
	return nil
}

// iterateCatchallLoose walks .md files sitting directly under the
// parent folder (not inside `index.md` or any declared sub-file dir)
// and attributes them to the catchall kind. The files stay where they
// are — mdql doesn't move them into the catchall Dir; the schema just
// knows what kind they are.
func iterateCatchallLoose(parentKind, parentSlug, catchallKind, parentDir string, entity schema.Entity, visit SubFileVisitor) error {
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil
	}
	declaredDirs := map[string]struct{}{}
	for _, f := range entity.Files {
		declaredDirs[f.Dir] = struct{}{}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if name == "index.md" || !strings.HasSuffix(name, ".md") {
			continue
		}
		if _, declared := declaredDirs[strings.TrimSuffix(name, ".md")]; declared {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		path := filepath.Join(parentDir, name)
		if err := emitSubFile(parentKind, parentSlug, catchallKind, slug, path, visit); err != nil {
			return err
		}
	}
	return nil
}

func emitSubFile(parentKind, parentSlug, subKind, slug, path string, visit SubFileVisitor) error {
	front, body, err := Parse(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("parse %s: %w", path, err)
	}
	fields, err := decodeMap(front)
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	fields["id"] = slug
	rec := SubFileRecord{
		Kind:       subKind,
		ParentKind: parentKind,
		ParentSlug: parentSlug,
		Slug:       slug,
		Path:       path,
		Fields:     fields,
		Body:       string(body),
	}
	return visit(rec, front, body)
}

// CreateSubFile writes a new sub-file record under the parent entity
// and returns the stored record. Requires the parent folder to exist
// (parent must be created first).
func (s *Store) CreateSubFile(ctx context.Context, parentKind, parentSlug, subKind string, input map[string]any) (SubFileRecord, error) {
	p, err := s.prepareSubFileCreate(ctx, parentKind, parentSlug, subKind, input)
	if err != nil {
		return SubFileRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p.rec.Path), 0o755); err != nil {
		return SubFileRecord{}, fmt.Errorf("mkdir %s: %w", filepath.Dir(p.rec.Path), err)
	}
	if err := writeEntity(p.rec.Path, frontmatterOnly(p.rec.Fields), p.rec.Body); err != nil {
		return SubFileRecord{}, err
	}
	if err := s.indexSubFile(parentKind, parentSlug, p.sub, p.rec); err != nil {
		return SubFileRecord{}, err
	}
	return p.rec, nil
}

// PlanCreateSubFile describes what CreateSubFile would write without
// touching disk or the indexer.
func (s *Store) PlanCreateSubFile(ctx context.Context, parentKind, parentSlug, subKind string, input map[string]any) (WritePlan, error) {
	p, err := s.prepareSubFileCreate(ctx, parentKind, parentSlug, subKind, input)
	if err != nil {
		return WritePlan{}, err
	}
	return WritePlan{
		Action:      "create",
		Kind:        subKind,
		ParentKind:  parentKind,
		ParentSlug:  parentSlug,
		Slug:        p.rec.Slug,
		Path:        s.relPath(p.rec.Path),
		Frontmatter: frontmatterOnly(p.rec.Fields),
		Body:        p.rec.Body,
	}, nil
}

type preparedSubFileCreate struct {
	sub schema.SubFile
	rec SubFileRecord
}

func (s *Store) prepareSubFileCreate(ctx context.Context, parentKind, parentSlug, subKind string, input map[string]any) (preparedSubFileCreate, error) {
	if err := ctxErr(ctx); err != nil {
		return preparedSubFileCreate{}, err
	}
	entity, sub, err := s.resolveSubFile(parentKind, subKind)
	if err != nil {
		return preparedSubFileCreate{}, err
	}
	parentDir := filepath.Join(s.root, entity.Dir, parentSlug)
	if _, err := os.Stat(parentDir); err != nil {
		if os.IsNotExist(err) {
			return preparedSubFileCreate{}, fmt.Errorf("%s/%s: %w", parentKind, parentSlug, model.ErrNotFound)
		}
		return preparedSubFileCreate{}, fmt.Errorf("stat parent %s: %w", parentDir, err)
	}

	rec := cloneMap(input)
	applySubFileDefaults(sub, rec)
	if err := validateSubFileRequired(parentKind, subKind, sub, rec); err != nil {
		return preparedSubFileCreate{}, err
	}
	if err := validateSubFileEnums(parentKind, subKind, sub, rec); err != nil {
		return preparedSubFileCreate{}, err
	}

	body, _ := rec[bodyKey].(string)
	delete(rec, bodyKey)

	if sub.Slug == "" {
		return preparedSubFileCreate{}, fmt.Errorf("sub-file %q has no slug template", subKind)
	}
	rendered, err := schema.Render(sub.Slug, rec)
	if err != nil {
		return preparedSubFileCreate{}, fmt.Errorf("render slug: %w", err)
	}
	baseSlug := Slugify(rendered)
	if baseSlug == "" {
		return preparedSubFileCreate{}, fmt.Errorf("empty slug from %q: %w", sub.Slug, model.ErrValidation)
	}
	dir := filepath.Join(parentDir, sub.Dir)
	slug, err := EnsureUnique(dir, baseSlug)
	if err != nil {
		return preparedSubFileCreate{}, err
	}
	rec["id"] = slug
	out := SubFileRecord{
		Kind:       subKind,
		ParentKind: parentKind,
		ParentSlug: parentSlug,
		Slug:       slug,
		Path:       filepath.Join(dir, slug+".md"),
		Fields:     rec,
		Body:       body,
	}
	return preparedSubFileCreate{sub: sub, rec: out}, nil
}

// indexSubFile is a thin wrapper so create/update share one upsert
// path. Keeps subFileDoc construction in one place and isolates the
// indexer call so later dry-run support (commit 5) can intercept it.
func (s *Store) indexSubFile(parentKind, parentSlug string, sub schema.SubFile, rec SubFileRecord) error {
	doc, err := subFileDoc(parentKind, parentSlug, sub, rec)
	if err != nil {
		return err
	}
	if err := s.indexer.Upsert(doc); err != nil {
		return fmt.Errorf("index sub-file: %w", err)
	}
	return nil
}

// GetSubFile returns the sub-file with the given slug.
func (s *Store) GetSubFile(ctx context.Context, parentKind, parentSlug, subKind, subSlug string) (SubFileRecord, error) {
	if err := ctxErr(ctx); err != nil {
		return SubFileRecord{}, err
	}
	path, err := s.SubFilePath(parentKind, parentSlug, subKind, subSlug)
	if err != nil {
		return SubFileRecord{}, err
	}
	front, body, err := Parse(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SubFileRecord{}, fmt.Errorf("%s/%s/%s/%s: %w", parentKind, parentSlug, subKind, subSlug, model.ErrNotFound)
		}
		return SubFileRecord{}, err
	}
	fields, err := decodeMap(front)
	if err != nil {
		return SubFileRecord{}, err
	}
	fields["id"] = subSlug
	return SubFileRecord{
		Kind:       subKind,
		ParentKind: parentKind,
		ParentSlug: parentSlug,
		Slug:       subSlug,
		Path:       path,
		Fields:     fields,
		Body:       string(body),
	}, nil
}

// ListSubFileGraph returns every sub-file under parentSlug across all
// declared sub-file kinds, in (kind, slug) order. Used by entity `show`
// to compose the full child-document graph for a parent entity; agents
// get one call that reveals the whole structure.
func (s *Store) ListSubFileGraph(ctx context.Context, parentKind, parentSlug string) ([]SubFileRecord, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	entity, ok := s.schema.Entities[parentKind]
	if !ok {
		return nil, fmt.Errorf("unknown entity kind %q", parentKind)
	}
	if !entity.IsSprawl() {
		return nil, nil
	}
	var out []SubFileRecord
	err := s.ForEachSubFile(parentKind, parentSlug, "", func(rec SubFileRecord, _, _ []byte) error {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		out = append(out, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListSubFiles returns every record of subKind under parentSlug.
func (s *Store) ListSubFiles(ctx context.Context, parentKind, parentSlug, subKind string) ([]SubFileRecord, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if _, _, err := s.resolveSubFile(parentKind, subKind); err != nil {
		return nil, err
	}
	var out []SubFileRecord
	err := s.ForEachSubFile(parentKind, parentSlug, subKind, func(rec SubFileRecord, _, _ []byte) error {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		out = append(out, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateSubFile applies patch to the existing sub-file record. Mirrors
// Update on the entity: nil values delete keys, everything else
// overwrites; `body` is applied as the markdown body.
func (s *Store) UpdateSubFile(ctx context.Context, parentKind, parentSlug, subKind, subSlug string, patch map[string]any) (SubFileRecord, error) {
	p, err := s.prepareSubFileUpdate(ctx, parentKind, parentSlug, subKind, subSlug, patch)
	if err != nil {
		return SubFileRecord{}, err
	}
	if err := writeEntity(p.rec.Path, frontmatterOnly(p.rec.Fields), p.rec.Body); err != nil {
		return SubFileRecord{}, err
	}
	if err := s.indexSubFile(parentKind, parentSlug, p.sub, p.rec); err != nil {
		return SubFileRecord{}, err
	}
	return p.rec, nil
}

// PlanUpdateSubFile describes what UpdateSubFile would change without
// touching disk or the indexer.
func (s *Store) PlanUpdateSubFile(ctx context.Context, parentKind, parentSlug, subKind, subSlug string, patch map[string]any) (WritePlan, error) {
	p, err := s.prepareSubFileUpdate(ctx, parentKind, parentSlug, subKind, subSlug, patch)
	if err != nil {
		return WritePlan{}, err
	}
	return WritePlan{
		Action:      "update",
		Kind:        subKind,
		ParentKind:  parentKind,
		ParentSlug:  parentSlug,
		Slug:        subSlug,
		Path:        s.relPath(p.rec.Path),
		Frontmatter: frontmatterOnly(p.rec.Fields),
		Body:        p.rec.Body,
		Before:      &PlanBefore{Frontmatter: p.beforeFront, Body: p.beforeBody},
	}, nil
}

type preparedSubFileUpdate struct {
	sub         schema.SubFile
	rec         SubFileRecord
	beforeFront map[string]any
	beforeBody  string
}

func (s *Store) prepareSubFileUpdate(ctx context.Context, parentKind, parentSlug, subKind, subSlug string, patch map[string]any) (preparedSubFileUpdate, error) {
	if err := ctxErr(ctx); err != nil {
		return preparedSubFileUpdate{}, err
	}
	_, sub, err := s.resolveSubFile(parentKind, subKind)
	if err != nil {
		return preparedSubFileUpdate{}, err
	}
	rec, err := s.GetSubFile(ctx, parentKind, parentSlug, subKind, subSlug)
	if err != nil {
		return preparedSubFileUpdate{}, err
	}
	beforeBody := rec.Body
	beforeFront := frontmatterOnly(rec.Fields)

	for k, v := range patch {
		if v == nil {
			delete(rec.Fields, k)
			continue
		}
		rec.Fields[k] = v
	}
	if err := validateSubFileEnums(parentKind, subKind, sub, rec.Fields); err != nil {
		return preparedSubFileUpdate{}, err
	}
	body, _ := rec.Fields[bodyKey].(string)
	delete(rec.Fields, bodyKey)
	if body == "" {
		body = rec.Body
	}
	rec.Fields["id"] = subSlug
	rec.Body = body
	return preparedSubFileUpdate{sub: sub, rec: rec, beforeFront: beforeFront, beforeBody: beforeBody}, nil
}

// DeleteSubFile removes the sub-file record. No soft-delete / archive —
// sub-files are usually artifacts that don't need the audit trail.
func (s *Store) DeleteSubFile(ctx context.Context, parentKind, parentSlug, subKind, subSlug string) error {
	p, err := s.prepareSubFileDelete(ctx, parentKind, parentSlug, subKind, subSlug)
	if err != nil {
		return err
	}
	if err := os.Remove(p.path); err != nil {
		return fmt.Errorf("remove %s: %w", p.path, err)
	}
	if err := s.indexer.Remove(subFileDocID(parentKind, parentSlug, subKind, subSlug)); err != nil {
		return fmt.Errorf("deindex sub-file: %w", err)
	}
	return nil
}

// PlanDeleteSubFile describes what DeleteSubFile would remove without
// touching disk or the indexer.
func (s *Store) PlanDeleteSubFile(ctx context.Context, parentKind, parentSlug, subKind, subSlug string) (WritePlan, error) {
	p, err := s.prepareSubFileDelete(ctx, parentKind, parentSlug, subKind, subSlug)
	if err != nil {
		return WritePlan{}, err
	}
	return WritePlan{
		Action:     "delete",
		Kind:       subKind,
		ParentKind: parentKind,
		ParentSlug: parentSlug,
		Slug:       subSlug,
		Path:       s.relPath(p.path),
		Before:     &PlanBefore{Frontmatter: p.beforeFront, Body: p.beforeBody},
	}, nil
}

type preparedSubFileDelete struct {
	path        string
	beforeFront map[string]any
	beforeBody  string
}

func (s *Store) prepareSubFileDelete(ctx context.Context, parentKind, parentSlug, subKind, subSlug string) (preparedSubFileDelete, error) {
	if err := ctxErr(ctx); err != nil {
		return preparedSubFileDelete{}, err
	}
	path, err := s.SubFilePath(parentKind, parentSlug, subKind, subSlug)
	if err != nil {
		return preparedSubFileDelete{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return preparedSubFileDelete{}, fmt.Errorf("%s/%s/%s/%s: %w", parentKind, parentSlug, subKind, subSlug, model.ErrNotFound)
		}
		return preparedSubFileDelete{}, fmt.Errorf("stat %s: %w", path, err)
	}
	rec, err := s.GetSubFile(ctx, parentKind, parentSlug, subKind, subSlug)
	if err != nil {
		return preparedSubFileDelete{}, err
	}
	return preparedSubFileDelete{
		path:        path,
		beforeFront: frontmatterOnly(rec.Fields),
		beforeBody:  rec.Body,
	}, nil
}

func applySubFileDefaults(sub schema.SubFile, rec map[string]any) {
	for name, field := range sub.Fields {
		if field.Default == nil {
			continue
		}
		if _, has := rec[name]; has && !isBlank(rec[name]) {
			continue
		}
		rec[name] = field.Default
	}
}

func validateSubFileRequired(parentKind, subKind string, sub schema.SubFile, rec map[string]any) error {
	for name, field := range sub.Fields {
		if !field.Required {
			continue
		}
		if isBlank(rec[name]) {
			return fmt.Errorf("%s.%s: field %q is required: %w", parentKind, subKind, name, model.ErrValidation)
		}
	}
	return nil
}

func validateSubFileEnums(parentKind, subKind string, sub schema.SubFile, rec map[string]any) error {
	for name, field := range sub.Fields {
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
			return fmt.Errorf("%s.%s: field %q=%q not in %v: %w", parentKind, subKind, name, val, field.Values, model.ErrValidation)
		}
	}
	return nil
}
