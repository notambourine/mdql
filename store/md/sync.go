package md

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// syncStampFile lives alongside the bleve index and records when the
// index was last fully reconciled against the filesystem. Separate from
// bleve's own index_meta.json (which stores storage/scorch config) so
// we don't collide with Bleve internals.
const syncStampFile = "sync.json"

type syncStamp struct {
	LastIndexedAt int64 `json:"last_indexed_at"`
}

// SyncIfStale reconciles the search index with the current filesystem
// state. Returns true if a rebuild was triggered.
//
// The check is a single-pass walk of the store root comparing each
// .md file's mtime against the stamp written by the last successful
// Reindex. First file newer than the stamp short-circuits the walk and
// triggers a full Reindex — strictly cheaper than the rebuild it
// avoids, since the rebuild itself walks+parses every file.
//
// Callers that write through mdql's own Create/Update don't need this
// (Upsert keeps the index consistent). It exists to repair drift from
// external mutations: `git pull`, hand-edited files, fresh clones
// where the index hasn't been populated yet.
//
// Excludes the runtime and archive dirs — bleve's own files mutate on
// every indexing operation, so including them would cause infinite
// auto-rebuild loops, and archived records don't participate in search.
func (s *Store) SyncIfStale(ctx context.Context) (bool, error) {
	runtimeDir := s.schema.Store.RuntimeDir
	archiveDir := s.schema.Store.ArchiveDir
	stampPath := filepath.Join(s.root, runtimeDir, syncStampFile)

	stamp, err := readSyncStamp(stampPath)
	if err != nil {
		return false, err
	}

	newest, err := newestMdMtime(s.root, runtimeDir, archiveDir, stamp.LastIndexedAt)
	if err != nil {
		return false, err
	}
	if newest <= stamp.LastIndexedAt {
		return false, nil
	}

	if _, err := s.Reindex(ctx); err != nil {
		return false, err
	}
	if err := writeSyncStamp(stampPath, time.Now().UnixNano()); err != nil {
		return true, err
	}
	return true, nil
}

// MarkSynced writes a fresh sync stamp without reindexing. Called by
// mdql init so the first post-init command doesn't trigger a rebuild
// of an empty store.
func (s *Store) MarkSynced() error {
	runtimeDir := s.schema.Store.RuntimeDir
	stampPath := filepath.Join(s.root, runtimeDir, syncStampFile)
	if err := os.MkdirAll(filepath.Dir(stampPath), 0o755); err != nil {
		return err
	}
	return writeSyncStamp(stampPath, time.Now().UnixNano())
}

func readSyncStamp(path string) (syncStamp, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return syncStamp{}, nil
		}
		return syncStamp{}, err
	}
	var s syncStamp
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupt stamp → treat as stale and rebuild. Better than bailing.
		return syncStamp{}, nil
	}
	return s, nil
}

func writeSyncStamp(path string, nanos int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(syncStamp{LastIndexedAt: nanos})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// newestMdMtime walks root looking for the first .md file with
// mtime > threshold. Returns as soon as one is found (short-circuit),
// or the newest mtime seen if none exceed threshold. Skips runtime
// and archive dirs.
func newestMdMtime(root, runtimeDir, archiveDir string, threshold int64) (int64, error) {
	var newest int64
	skip := map[string]bool{}
	if runtimeDir != "" {
		skip[filepath.Join(root, runtimeDir)] = true
	}
	if archiveDir != "" {
		skip[filepath.Join(root, archiveDir)] = true
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[path] {
				return fs.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		m := info.ModTime().UnixNano()
		if m > newest {
			newest = m
		}
		if m > threshold {
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return 0, walkErr
	}
	return newest, nil
}
