package md

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/notambourine/mdql/schema"
)

// Init scaffolds a fresh store under root against the given schema.
// Creates one directory per schema entity, the archive tree, the
// runtime dir (e.g. .mdql), and a .gitignore that hides runtime state.
// Idempotent: safe to run against an existing tree.
func Init(root string, s *schema.Schema) error {
	if root == "" {
		return fmt.Errorf("init: empty root")
	}
	if s == nil {
		return fmt.Errorf("init: nil schema")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create root %s: %w", root, err)
	}

	for _, entity := range s.Entities {
		if err := os.MkdirAll(filepath.Join(root, entity.Dir), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", entity.Dir, err)
		}
		if err := os.MkdirAll(filepath.Join(root, s.Store.ArchiveDir, entity.Dir), 0o755); err != nil {
			return fmt.Errorf("create archive/%s: %w", entity.Dir, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(root, s.Store.RuntimeDir), 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	gitignorePath := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		contents := s.Store.RuntimeDir + "/\n"
		if err := os.WriteFile(gitignorePath, []byte(contents), 0o644); err != nil {
			return fmt.Errorf("write .gitignore: %w", err)
		}
	}

	return nil
}

// EntityDir returns the absolute directory path for kind.
// Errors if kind is not declared in the schema.
func (s *Store) EntityDir(kind string) (string, error) {
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return "", fmt.Errorf("unknown entity kind %q", kind)
	}
	return filepath.Join(s.root, entity.Dir), nil
}

// EntityPath returns the absolute file path for a slug of the given kind.
func (s *Store) EntityPath(kind, slug string) (string, error) {
	dir, err := s.EntityDir(kind)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slug+".md"), nil
}
