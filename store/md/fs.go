package md

import (
	"fmt"
	"os"
	"path/filepath"
)

// Entity kinds map to top-level directories under the store root.
const (
	KindPerson      = "person"
	KindOrg         = "organization"
	KindDeal        = "deal"
	KindTask        = "task"
	KindInteraction = "interaction"
)

// Dirs maps each entity kind to its directory name under the store root.
var Dirs = map[string]string{
	KindPerson:      "people",
	KindOrg:         "organizations",
	KindDeal:        "deals",
	KindTask:        "tasks",
	KindInteraction: "interactions",
}

// ArchiveDir is the top-level directory that holds soft-deleted entries.
const ArchiveDir = "_archive"

// RuntimeDir is a gitignored directory holding the bleve index and cache.
const RuntimeDir = ".crm"

// gitignoreContents keeps runtime state out of version control.
const gitignoreContents = RuntimeDir + "/\n"

// Init scaffolds a fresh store under root. Creates entity directories,
// the archive tree, the runtime dir, and a .gitignore that hides runtime
// state. Idempotent: safe to run against an existing tree.
func Init(root string) error {
	if root == "" {
		return fmt.Errorf("init: empty root")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create root %s: %w", root, err)
	}

	for _, dir := range Dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.MkdirAll(filepath.Join(root, ArchiveDir, dir), 0o755); err != nil {
			return fmt.Errorf("create archive/%s: %w", dir, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(root, RuntimeDir), 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	gitignorePath := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte(gitignoreContents), 0o644); err != nil {
			return fmt.Errorf("write .gitignore: %w", err)
		}
	}

	return nil
}

// EntityDir returns the absolute directory path for kind under root.
// Returns an error for unknown kinds to catch typos at the boundary.
func EntityDir(root, kind string) (string, error) {
	dir, ok := Dirs[kind]
	if !ok {
		return "", fmt.Errorf("unknown entity kind %q", kind)
	}
	return filepath.Join(root, dir), nil
}

// EntityPath returns the absolute file path for a slug of the given kind.
func EntityPath(root, kind, slug string) (string, error) {
	dir, err := EntityDir(root, kind)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slug+".md"), nil
}
