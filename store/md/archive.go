package md

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNotArchivable is returned when the source file does not exist.
var ErrNotArchivable = errors.New("entity file not found")

// Archive soft-deletes an entity by moving it under the archive tree.
// Sprawl entities move the whole `{dir}/{slug}/` folder (sub-files,
// notes, attachments included); flat entities move the single
// `{dir}/{slug}.md` file. Git records either as a rename so the diff
// stays readable.
//
// Returns ErrNotArchivable wrapped when the source doesn't exist, so
// callers can distinguish "already archived / never existed" from I/O
// errors.
func (s *Store) Archive(kind, slug string) error {
	src, dst, err := s.archiveEndpoints(kind, slug)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s/%s: %w", kind, slug, ErrNotArchivable)
		}
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir archive dir: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename to archive: %w", err)
	}
	return nil
}

// Restore is the inverse of Archive: moves the file or folder back out
// of the archive tree.
func (s *Store) Restore(kind, slug string) error {
	dst, src, err := s.archiveEndpoints(kind, slug)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("archive/%s/%s: %w", kind, slug, ErrNotArchivable)
		}
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir restore dir: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename from archive: %w", err)
	}
	return nil
}

// archiveEndpoints returns the live path and archive path for an entity.
// For sprawl entities both are folders (`{dir}/{slug}/` and
// `{archive}/{dir}/{slug}/`); for flat entities both are files. Callers
// rename in either direction — Restore just swaps the return order.
func (s *Store) archiveEndpoints(kind, slug string) (live, archived string, err error) {
	entity, ok := s.schema.Entities[kind]
	if !ok {
		return "", "", fmt.Errorf("unknown entity kind %q", kind)
	}
	archRoot := filepath.Join(s.root, s.schema.Store.ArchiveDir, entity.Dir)
	liveRoot := filepath.Join(s.root, entity.Dir)
	if entity.IsSprawl() {
		return filepath.Join(liveRoot, slug), filepath.Join(archRoot, slug), nil
	}
	return filepath.Join(liveRoot, slug+".md"), filepath.Join(archRoot, slug+".md"), nil
}
