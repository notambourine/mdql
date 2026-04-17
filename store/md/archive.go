package md

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNotArchivable is returned when the source file does not exist.
var ErrNotArchivable = errors.New("entity file not found")

// Archive moves <kind-dir>/<slug>.md to <archive>/<kind-dir>/<slug>.md.
// Git records this as a rename, so the soft-delete is diff-friendly.
//
// Returns ErrNotArchivable wrapped when the source doesn't exist, so
// callers can distinguish "already archived / never existed" from I/O
// errors.
func (s *Store) Archive(kind, slug string) error {
	src, err := s.EntityPath(kind, slug)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s/%s: %w", kind, slug, ErrNotArchivable)
		}
		return fmt.Errorf("stat %s: %w", src, err)
	}

	entity, ok := s.schema.Entities[kind]
	if !ok {
		return fmt.Errorf("unknown entity kind %q", kind)
	}
	dstDir := filepath.Join(s.root, s.schema.Store.ArchiveDir, entity.Dir)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("mkdir archive dir: %w", err)
	}

	dst := filepath.Join(dstDir, slug+".md")
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename to archive: %w", err)
	}
	return nil
}

// Restore is the inverse of Archive: moves the file back out of the
// archive tree.
func (s *Store) Restore(kind, slug string) error {
	dst, err := s.EntityPath(kind, slug)
	if err != nil {
		return err
	}
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return fmt.Errorf("unknown entity kind %q", kind)
	}
	src := filepath.Join(s.root, s.schema.Store.ArchiveDir, entity.Dir, slug+".md")
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("archive/%s/%s: %w", kind, slug, ErrNotArchivable)
		}
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename from archive: %w", err)
	}
	return nil
}
