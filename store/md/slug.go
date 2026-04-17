package md

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	slugSeparator = regexp.MustCompile(`[\s_]+`)
	slugInvalid   = regexp.MustCompile(`[^a-z0-9-]+`)
	slugDashes    = regexp.MustCompile(`-+`)
)

// Slugify converts a free-form name into a filesystem-safe slug.
//
// "Jane Smith" → "jane-smith"; "Acme Corp!" → "acme-corp";
// "São Paulo" → "so-paulo" (diacritics stripped by ASCII-only rules,
// documented as a known limitation — v2.1 can add unicode normalization
// if it becomes a real problem).
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugSeparator.ReplaceAllString(s, "-")
	s = slugInvalid.ReplaceAllString(s, "")
	s = slugDashes.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// ErrEmptySlug is returned when Slugify produces an empty string.
var ErrEmptySlug = errors.New("cannot generate slug from input")

// EnsureUnique returns a slug that does not collide with an existing file
// in dir. If "jane-smith.md" exists, returns "jane-smith-2"; if that
// exists too, "jane-smith-3", and so on.
//
// Two concurrent callers seeing the same dir state may both settle on
// the same suffix — the caller is responsible for using atomic creation
// (O_CREATE|O_EXCL) to break ties.
func EnsureUnique(dir, slug string) (string, error) {
	return ensureUniqueBy(slug, func(candidate string) string {
		return filepath.Join(dir, candidate+".md")
	})
}

// EnsureUniqueFolder is the sprawl-mode sibling of EnsureUnique: checks
// for subfolder collisions under dir. If `dir/jane-smith/` exists,
// returns "jane-smith-2"; etc.
func EnsureUniqueFolder(dir, slug string) (string, error) {
	return ensureUniqueBy(slug, func(candidate string) string {
		return filepath.Join(dir, candidate)
	})
}

func ensureUniqueBy(slug string, pathFor func(string) string) (string, error) {
	if slug == "" {
		return "", ErrEmptySlug
	}
	candidate := slug
	for i := 2; ; i++ {
		path := pathFor(candidate)
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", path, err)
		}
		candidate = slug + "-" + strconv.Itoa(i)
	}
}
