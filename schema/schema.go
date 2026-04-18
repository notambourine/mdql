// Package schema defines the YAML schema that drives mdql's generic
// CRUD, indexing, and CLI generation. A schema is loaded once per
// binary invocation and walked by the runtime to produce cobra
// commands, index docs, and validation rules.
package schema

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Schema is the parsed form of a schema.yml file.
type Schema struct {
	Version  int               `yaml:"version"`
	Store    StoreConfig       `yaml:"store"`
	Entities map[string]Entity `yaml:"entities"`
}

// StoreConfig controls where mdql keeps derived state on disk.
type StoreConfig struct {
	RuntimeDir string `yaml:"runtime_dir" json:"runtime_dir"`
	ArchiveDir string `yaml:"archive_dir" json:"archive_dir"`
}

// Entity is one schema-defined record kind (e.g. person, deal).
type Entity struct {
	Dir        string `yaml:"dir"`
	Title      string `yaml:"title"`
	Slug       string `yaml:"slug"`
	Archivable *bool  `yaml:"archivable"`
	AppendOnly bool   `yaml:"append_only"`
	// Flat opts out of sprawl: when true, the entity stores one file per
	// record at `{dir}/{slug}.md` instead of a folder per record at
	// `{dir}/{slug}/index.md`. Default is sprawl; `flat: true` is the
	// escape hatch for entity kinds that will never grow sub-files.
	Flat   bool             `yaml:"flat"`
	Fields map[string]Field `yaml:"fields"`
	// Files declares typed sub-file kinds that live alongside index.md
	// inside a sprawl entity folder. Each SubFile has its own CRUD verbs
	// (mdql <entity> <subkind> add/list/show/update/delete). Only sprawl
	// entities may declare files; flat entities have no folder to host
	// siblings.
	Files map[string]SubFile `yaml:"files"`
}

// SubFile declares a typed sub-file kind inside a sprawl entity folder.
// A sub-file record lives at `{entity_dir}/{parent_slug}/{subfile.Dir}/
// {sub_slug}.md` with YAML frontmatter matching SubFile.Fields.
//
// Exactly one SubFile per Entity may set Catchall=true: that kind
// absorbs any untyped .md files dropped under the entity folder (they
// stay in place physically — the schema just knows what kind they are
// for indexing, wiki backlinks, and lint reporting).
type SubFile struct {
	Dir      string           `yaml:"dir"`
	Slug     string           `yaml:"slug"`
	Title    string           `yaml:"title"`
	Catchall bool             `yaml:"catchall"`
	Fields   map[string]Field `yaml:"fields"`
}

// IsArchivable returns the effective archivable flag (default true).
func (e Entity) IsArchivable() bool {
	if e.Archivable == nil {
		return true
	}
	return *e.Archivable
}

// IsSprawl reports whether the entity uses folder-per-record layout.
// Default is sprawl; opt out with `flat: true` in the schema.
func (e Entity) IsSprawl() bool { return !e.Flat }

// Field describes one frontmatter attribute on an entity.
type Field struct {
	Type      string   `yaml:"type"`
	Required  bool     `yaml:"required"`
	Unique    bool     `yaml:"unique"`
	Sorted    bool     `yaml:"sorted"`
	Default   any      `yaml:"default"`
	Values    []string `yaml:"values"`
	Target    string   `yaml:"target"`
	EdgeTypes []string `yaml:"edge_types"`
}

// allowedFieldTypes is the set of field.Type strings v1 accepts.
var allowedFieldTypes = map[string]struct{}{
	"string":     {},
	"int":        {},
	"float":      {},
	"bool":       {},
	"date":       {},
	"enum":       {},
	"link":       {},
	"string[]":   {},
	"link[]":     {},
	"relation[]": {},
}

// Parse decodes YAML bytes into a Schema, applies defaults, and
// validates it. The returned schema is ready to hand to the runtime.
func Parse(data []byte) (*Schema, error) {
	var s Schema
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("schema: unmarshal: %w", err)
	}
	s.applyDefaults()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *Schema) applyDefaults() {
	if s.Store.RuntimeDir == "" {
		s.Store.RuntimeDir = ".mdql"
	}
	if s.Store.ArchiveDir == "" {
		s.Store.ArchiveDir = "_archive"
	}
	for name, entity := range s.Entities {
		if entity.Archivable == nil {
			t := true
			entity.Archivable = &t
			s.Entities[name] = entity
		}
	}
}

// Validate returns an error if the schema is structurally invalid.
// Called by Parse; safe to re-run after mutation.
func (s *Schema) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("schema: unsupported version %d (want 1)", s.Version)
	}
	if len(s.Entities) == 0 {
		return fmt.Errorf("schema: no entities defined")
	}
	for name, entity := range s.Entities {
		if entity.Dir == "" {
			return fmt.Errorf("schema: entity %q: dir is required", name)
		}
		if entity.Title == "" {
			return fmt.Errorf("schema: entity %q: title is required", name)
		}
		if entity.Slug == "" {
			return fmt.Errorf("schema: entity %q: slug is required", name)
		}
		for fname, field := range entity.Fields {
			if err := validateField(name, fname, field, s.Entities); err != nil {
				return err
			}
		}
		if err := validateSubFiles(name, entity); err != nil {
			return err
		}
	}
	return nil
}

func validateField(entity, name string, f Field, entities map[string]Entity) error {
	if _, ok := allowedFieldTypes[f.Type]; !ok {
		return fmt.Errorf("schema: entity %q field %q: unknown type %q", entity, name, f.Type)
	}
	switch f.Type {
	case "enum":
		if len(f.Values) == 0 {
			return fmt.Errorf("schema: entity %q field %q: enum requires values", entity, name)
		}
	case "link", "link[]":
		if f.Target == "" {
			return fmt.Errorf("schema: entity %q field %q: %s requires target", entity, name, f.Type)
		}
		if !targetExists(f.Target, entities) {
			return fmt.Errorf("schema: entity %q field %q: target %q not defined", entity, name, f.Target)
		}
	case "relation[]":
		if f.Target == "" {
			return fmt.Errorf("schema: entity %q field %q: relation[] requires target", entity, name)
		}
		if !targetExists(f.Target, entities) {
			return fmt.Errorf("schema: entity %q field %q: target %q not defined", entity, name, f.Target)
		}
		if len(f.EdgeTypes) == 0 {
			return fmt.Errorf("schema: entity %q field %q: relation[] requires edge_types", entity, name)
		}
	}
	return nil
}

// targetExists accepts either a bare entity name ("person") or the
// dotted entity.subkind form ("project.decision") used to link to
// sub-files. Bare names match Schema.Entities; dotted names must
// resolve to an entity with that Files key.
func targetExists(target string, entities map[string]Entity) bool {
	if dot := strings.IndexByte(target, '.'); dot > 0 {
		parent, sub := target[:dot], target[dot+1:]
		ent, ok := entities[parent]
		if !ok {
			return false
		}
		_, ok = ent.Files[sub]
		return ok
	}
	_, ok := entities[target]
	return ok
}

// validateSubFiles enforces the rules on entity.files: blocks.
//   - only sprawl entities may declare files (flat has no folder to host them)
//   - each sub-file needs a dir
//   - non-catchall sub-files need a slug template
//   - at most one catchall per entity
//   - sub-file field types follow the same rules as entity fields
func validateSubFiles(entityName string, entity Entity) error {
	if len(entity.Files) == 0 {
		return nil
	}
	if !entity.IsSprawl() {
		return fmt.Errorf("schema: entity %q: files: requires sprawl (remove flat: true)", entityName)
	}
	seenCatchall := ""
	dirs := map[string]string{}
	for subName, sub := range entity.Files {
		if sub.Dir == "" {
			return fmt.Errorf("schema: entity %q file %q: dir is required", entityName, subName)
		}
		if prior, ok := dirs[sub.Dir]; ok {
			return fmt.Errorf("schema: entity %q files %q and %q share dir %q", entityName, prior, subName, sub.Dir)
		}
		dirs[sub.Dir] = subName
		if sub.Catchall {
			if seenCatchall != "" {
				return fmt.Errorf("schema: entity %q: multiple catchall files (%q and %q)", entityName, seenCatchall, subName)
			}
			seenCatchall = subName
		} else if sub.Slug == "" {
			return fmt.Errorf("schema: entity %q file %q: slug is required (use catchall: true to opt out)", entityName, subName)
		}
	}
	return nil
}
