package md

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const subFileFixtureYAML = `
version: 1
entities:
  project:
    dir: projects
    title: "{{.name}}"
    slug:  "{{.name}}"
    fields:
      name: {type: string, required: true}
    files:
      meeting:
        dir: meetings
        slug: "{{.date}}-{{.subject}}"
        fields:
          subject: {type: string, required: true}
          date:    {type: string, required: true}
      decision:
        dir: decisions
        slug: "{{.title}}"
        fields:
          title: {type: string, required: true}
      note:
        catchall: true
        dir: notes
        fields:
          title: {type: string}
`

func newSubFileStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	sch := mustParseSchema(t, subFileFixtureYAML)
	require.NoError(t, Init(root, sch))
	s, err := Open(root, sch, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.Create(context.Background(), "project", map[string]any{"name": "Launch Site"})
	require.NoError(t, err)
	return s
}

func TestSubFileCreate(t *testing.T) {
	s := newSubFileStore(t)
	rec, err := s.CreateSubFile(context.Background(), "project", "launch-site", "meeting", map[string]any{
		"subject": "kickoff",
		"date":    "2026-04-17",
	})
	require.NoError(t, err)
	assert.Equal(t, "2026-04-17-kickoff", rec.Slug)
	assert.Equal(t, "meeting", rec.Kind)
	assert.Equal(t, "launch-site", rec.ParentSlug)

	want := filepath.Join(s.Root(), "projects", "launch-site", "meetings", "2026-04-17-kickoff.md")
	_, err = os.Stat(want)
	require.NoError(t, err)
}

func TestSubFileCreateRequired(t *testing.T) {
	s := newSubFileStore(t)
	_, err := s.CreateSubFile(context.Background(), "project", "launch-site", "meeting", map[string]any{
		"date": "2026-04-17",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subject")
}

func TestSubFileCreateParentMissing(t *testing.T) {
	s := newSubFileStore(t)
	_, err := s.CreateSubFile(context.Background(), "project", "ghost", "meeting", map[string]any{
		"subject": "x", "date": "2026-04-17",
	})
	require.Error(t, err)
}

func TestSubFileRoundtrip(t *testing.T) {
	s := newSubFileStore(t)
	_, err := s.CreateSubFile(context.Background(), "project", "launch-site", "meeting", map[string]any{
		"subject": "kickoff",
		"date":    "2026-04-17",
	})
	require.NoError(t, err)

	got, err := s.GetSubFile(context.Background(), "project", "launch-site", "meeting", "2026-04-17-kickoff")
	require.NoError(t, err)
	assert.Equal(t, "kickoff", got.Fields["subject"])

	_, err = s.UpdateSubFile(context.Background(), "project", "launch-site", "meeting", "2026-04-17-kickoff", map[string]any{
		"subject": "kickoff-v2",
	})
	require.NoError(t, err)
	got, err = s.GetSubFile(context.Background(), "project", "launch-site", "meeting", "2026-04-17-kickoff")
	require.NoError(t, err)
	assert.Equal(t, "kickoff-v2", got.Fields["subject"])

	require.NoError(t, s.DeleteSubFile(context.Background(), "project", "launch-site", "meeting", "2026-04-17-kickoff"))
	_, err = s.GetSubFile(context.Background(), "project", "launch-site", "meeting", "2026-04-17-kickoff")
	require.Error(t, err)
}

func TestSubFileList(t *testing.T) {
	s := newSubFileStore(t)
	_, _ = s.CreateSubFile(context.Background(), "project", "launch-site", "meeting", map[string]any{
		"subject": "kickoff", "date": "2026-04-17",
	})
	_, _ = s.CreateSubFile(context.Background(), "project", "launch-site", "meeting", map[string]any{
		"subject": "review", "date": "2026-04-20",
	})
	_, _ = s.CreateSubFile(context.Background(), "project", "launch-site", "decision", map[string]any{
		"title": "use-hugo",
	})

	meetings, err := s.ListSubFiles(context.Background(), "project", "launch-site", "meeting")
	require.NoError(t, err)
	assert.Len(t, meetings, 2)

	decisions, err := s.ListSubFiles(context.Background(), "project", "launch-site", "decision")
	require.NoError(t, err)
	assert.Len(t, decisions, 1)
}

// TestSubFileCatchallAbsorbsLooseFile drops an untyped .md into the
// parent folder and verifies ForEachSubFile with an empty filter
// attributes it to the catchall kind.
func TestSubFileCatchallAbsorbsLooseFile(t *testing.T) {
	s := newSubFileStore(t)
	loose := filepath.Join(s.Root(), "projects", "launch-site", "random-brain-dump.md")
	require.NoError(t, os.WriteFile(loose, []byte("---\ntitle: braindump\n---\n"), 0o644))

	var loose_seen bool
	err := s.ForEachSubFile("project", "launch-site", "", func(rec SubFileRecord, _, _ []byte) error {
		if rec.Slug == "random-brain-dump" {
			loose_seen = true
			assert.Equal(t, "note", rec.Kind)
		}
		return nil
	})
	require.NoError(t, err)
	assert.True(t, loose_seen, "loose file should be absorbed by catchall")
}
