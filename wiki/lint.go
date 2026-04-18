package wiki

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/notambourine/mdql/schema"
	"github.com/notambourine/mdql/store/md"
)

// Finding is one lint observation: one severity-tagged issue about one
// record or path. Scoped identifiers (kind/slug/sub_kind/sub_slug) are
// optional — cross-cutting checks like dangling_link may only carry the
// source, not every axis.
type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Kind     string `json:"kind,omitempty"`
	Slug     string `json:"slug,omitempty"`
	SubKind  string `json:"sub_kind,omitempty"`
	SubSlug  string `json:"sub_slug,omitempty"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

// Summary counts findings by severity. Callers derive exit codes from it
// rather than iterating Findings themselves.
type Summary struct {
	Info  int `json:"info"`
	Warn  int `json:"warn"`
	Error int `json:"error"`
}

// Report is the full Lint payload: every Finding plus a tally.
type Report struct {
	Findings []Finding `json:"findings"`
	Summary  Summary   `json:"summary"`
}

// Lint audits the store for drift between schema and disk. Finding codes:
//
//	INFO  catchall_absorbed  loose .md file attributed to a catchall kind
//	WARN  missing_required   record missing a required frontmatter field
//	WARN  orphan_folder      entity folder without index.md, no sub-data
//	WARN  dangling_link      [[slug]] target not found in any entity dir
//	ERROR orphan_subfiles    entity folder without index.md but holds data
//
// The Findings list is sorted (severity, code, kind, slug, sub_kind,
// sub_slug) so the report is deterministic across runs.
func Lint(ctx context.Context, s *md.Store) (*Report, error) {
	r := &Report{Findings: []Finding{}}
	sch := s.Schema()
	root := s.Root()

	kinds := make([]string, 0, len(sch.Entities))
	for kind := range sch.Entities {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	for _, kind := range kinds {
		entity := sch.Entities[kind]

		if entity.IsSprawl() {
			if err := scanOrphanFolders(root, kind, entity, r); err != nil {
				return nil, err
			}
		}

		err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
			rec, err := parseFrontMap(front)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", kind, slug, err)
			}
			rel, _ := filepath.Rel(root, path)
			for _, f := range missingRequired(entity.Fields, rec) {
				r.Findings = append(r.Findings, Finding{
					Severity: "warn", Code: "missing_required",
					Kind: kind, Slug: slug, Path: rel,
					Message: fmt.Sprintf("field %q required but missing", f),
				})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}

		if entity.IsSprawl() && len(entity.Files) > 0 {
			err := s.ForEachEntity(kind, func(slug, path string, front, body []byte) error {
				parentFolder := filepath.Join(entity.Dir, slug)
				return s.ForEachSubFile(kind, slug, "", func(rec md.SubFileRecord, sfFront, sfBody []byte) error {
					sub := entity.Files[rec.Kind]
					rel, _ := filepath.Rel(root, rec.Path)
					for _, f := range missingRequired(sub.Fields, rec.Fields) {
						r.Findings = append(r.Findings, Finding{
							Severity: "warn", Code: "missing_required",
							Kind: kind, Slug: slug, SubKind: rec.Kind, SubSlug: rec.Slug, Path: rel,
							Message: fmt.Sprintf("field %q required but missing", f),
						})
					}
					if sub.Catchall && filepath.Dir(rel) == parentFolder {
						r.Findings = append(r.Findings, Finding{
							Severity: "info", Code: "catchall_absorbed",
							Kind: kind, Slug: slug, SubKind: rec.Kind, SubSlug: rec.Slug, Path: rel,
							Message: fmt.Sprintf("loose .md attributed to catchall kind %q", rec.Kind),
						})
					}
					return nil
				})
			})
			if err != nil {
				return nil, err
			}
		}
	}

	dangling, err := Check(ctx, s)
	if err != nil {
		return nil, err
	}
	for _, d := range dangling {
		r.Findings = append(r.Findings, Finding{
			Severity: "warn", Code: "dangling_link",
			Kind: d.SourceType, Slug: d.SourceSlug,
			Message: fmt.Sprintf("link target %q not found", d.TargetSlug),
		})
	}

	sortFindings(r.Findings)
	for _, f := range r.Findings {
		switch f.Severity {
		case "info":
			r.Summary.Info++
		case "warn":
			r.Summary.Warn++
		case "error":
			r.Summary.Error++
		}
	}
	return r, nil
}

// scanOrphanFolders walks the entity dir and emits a finding for every
// subfolder missing index.md — WARN when the folder is empty, ERROR
// when it contains sub-files whose parent record no longer exists.
func scanOrphanFolders(root, kind string, entity schema.Entity, r *Report) error {
	entDir := filepath.Join(root, entity.Dir)
	entries, err := os.ReadDir(entDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", entDir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		folder := filepath.Join(entDir, slug)
		if _, err := os.Stat(filepath.Join(folder, "index.md")); err == nil {
			continue
		}
		rel, _ := filepath.Rel(root, folder)
		hasData, err := folderHasMarkdown(folder)
		if err != nil {
			return err
		}
		if hasData {
			r.Findings = append(r.Findings, Finding{
				Severity: "error", Code: "orphan_subfiles",
				Kind: kind, Slug: slug, Path: rel,
				Message: "sub-files present but index.md missing",
			})
		} else {
			r.Findings = append(r.Findings, Finding{
				Severity: "warn", Code: "orphan_folder",
				Kind: kind, Slug: slug, Path: rel,
				Message: "empty entity folder without index.md",
			})
		}
	}
	return nil
}

// folderHasMarkdown reports whether any .md file other than the folder's
// own index.md lives under folder (at any depth). Used by the orphan-
// folder classifier to distinguish "empty shell" (WARN) from "orphaned
// data" (ERROR).
func folderHasMarkdown(folder string) (bool, error) {
	var found bool
	err := filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		if filepath.Dir(p) == folder && d.Name() == "index.md" {
			return nil
		}
		found = true
		return filepath.SkipAll
	})
	return found, err
}

// parseFrontMap decodes YAML frontmatter into a generic map. A missing
// or empty frontmatter block is treated as an empty record so the
// required-field scan still fires on every declared required field.
func parseFrontMap(front []byte) (map[string]any, error) {
	out := map[string]any{}
	if len(front) == 0 {
		return out, nil
	}
	if err := yaml.Unmarshal(front, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// missingRequired returns the alphabetically-sorted names of fields
// declared required that are absent or empty in rec.
func missingRequired(fields map[string]schema.Field, rec map[string]any) []string {
	names := make([]string, 0, len(fields))
	for n, f := range fields {
		if f.Required {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	missing := make([]string, 0, len(names))
	for _, n := range names {
		v, ok := rec[n]
		if !ok || isBlank(v) {
			missing = append(missing, n)
		}
	}
	return missing
}

func isBlank(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

// sortFindings orders by severity (error > warn > info), then the
// scoped identity axes, so two runs with the same store produce the
// same report bytes.
func sortFindings(fs []Finding) {
	rank := map[string]int{"error": 0, "warn": 1, "info": 2}
	sort.SliceStable(fs, func(i, j int) bool {
		if ri, rj := rank[fs[i].Severity], rank[fs[j].Severity]; ri != rj {
			return ri < rj
		}
		if fs[i].Code != fs[j].Code {
			return fs[i].Code < fs[j].Code
		}
		if fs[i].Kind != fs[j].Kind {
			return fs[i].Kind < fs[j].Kind
		}
		if fs[i].Slug != fs[j].Slug {
			return fs[i].Slug < fs[j].Slug
		}
		if fs[i].SubKind != fs[j].SubKind {
			return fs[i].SubKind < fs[j].SubKind
		}
		return fs[i].SubSlug < fs[j].SubSlug
	})
}
