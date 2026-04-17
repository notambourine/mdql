package md

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// frontDelim separates the YAML frontmatter from the body.
var frontDelim = []byte("---\n")

// Parse splits a markdown file into YAML frontmatter bytes and body bytes.
//
// Files without frontmatter (no leading "---") are returned with an empty
// frontmatter slice and the full contents as body.
func Parse(path string) (front, body []byte, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	return split(raw)
}

// ParseBytes is Parse for in-memory buffers (used by tests and bleve index).
func ParseBytes(raw []byte) (front, body []byte, err error) {
	return split(raw)
}

func split(raw []byte) (front, body []byte, err error) {
	if !bytes.HasPrefix(raw, frontDelim) {
		return nil, raw, nil
	}
	rest := raw[len(frontDelim):]
	end := bytes.Index(rest, frontDelim)
	if end < 0 {
		return nil, nil, fmt.Errorf("unterminated frontmatter")
	}
	front = rest[:end]
	body = rest[end+len(frontDelim):]
	body = bytes.TrimPrefix(body, []byte("\n"))
	// Trim a single trailing newline so in-memory body matches what the
	// caller wrote; Write always ensures the file ends with a newline.
	body = bytes.TrimSuffix(body, []byte("\n"))
	return front, body, nil
}

// Write atomically writes frontmatter + body to path.
//
// front should be the YAML-encoded frontmatter bytes (without the delimiter
// fences). body may be empty. Directory is created if missing.
func Write(path string, front, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	var buf bytes.Buffer
	buf.Write(frontDelim)
	buf.Write(front)
	if len(front) > 0 && !bytes.HasSuffix(front, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.Write(frontDelim)
	if len(body) > 0 {
		buf.WriteByte('\n')
		buf.Write(body)
		if !bytes.HasSuffix(body, []byte("\n")) {
			buf.WriteByte('\n')
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
