package md

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/notambourine/mdql/model"
	"github.com/notambourine/mdql/store"
)

// recordingIndexer captures Upsert/Remove calls for assertions.
type recordingIndexer struct {
	upserts []store.Doc
	removed []string
}

func (r *recordingIndexer) Upsert(d store.Doc) error { r.upserts = append(r.upserts, d); return nil }
func (r *recordingIndexer) Remove(id string) error   { r.removed = append(r.removed, id); return nil }

// genericSchemaYAML exercises more field types than the base fixture:
// required, unique, enum, default, link, link[], relation[], string[]
// sorted — enough to cover every validation path.
const genericSchemaYAML = `
version: 1
entities:
  person:
    dir: people
    title: "{{.first_name}} {{.last_name}}"
    slug:  "{{.first_name}} {{.last_name}}"
    fields:
      first_name: {type: string, required: true}
      last_name:  {type: string}
      email:      {type: string, unique: true}
      org:        {type: link, target: organization}
      tags:       {type: "string[]", sorted: true}
  organization:
    dir: organizations
    title: "{{.name}}"
    slug:  "{{.name}}"
    fields:
      name: {type: string, required: true}
  deal:
    dir: deals
    title: "{{.title}}"
    slug:  "{{.title}}"
    fields:
      title: {type: string, required: true}
      stage: {type: enum, values: [lead, won, lost], required: true, default: lead}
      value: {type: float}
      people: {type: "link[]", target: person}
`

func newGenericStore(t *testing.T) (*Store, *recordingIndexer) {
	t.Helper()
	sch := mustParseSchema(t, genericSchemaYAML)
	root := t.TempDir()
	require.NoError(t, Init(root, sch))
	idx := &recordingIndexer{}
	s, err := Open(root, sch, idx)
	require.NoError(t, err)
	return s, idx
}

func TestCreateRoundtrip(t *testing.T) {
	s, idx := newGenericStore(t)
	ctx := context.Background()

	rec, err := s.Create(ctx, "person", map[string]any{
		"first_name": "Jane",
		"last_name":  "Smith",
		"email":      "jane@example.com",
		"body":       "# Notes\nhello",
	})
	require.NoError(t, err)
	assert.Equal(t, "jane-smith", rec["id"])
	assert.NotEmpty(t, rec["uuid"])
	assert.Equal(t, rec["created_at"], rec["updated_at"])

	path := filepath.Join(s.Root(), "people", "jane-smith.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "first_name: Jane")
	assert.Contains(t, string(raw), "# Notes\nhello")

	require.Len(t, idx.upserts, 1)
	assert.Equal(t, "person:jane-smith", idx.upserts[0].ID)
	assert.Equal(t, "Jane Smith", idx.upserts[0].Title)
}

func TestCreateRequiredMissing(t *testing.T) {
	s, _ := newGenericStore(t)
	_, err := s.Create(context.Background(), "person", map[string]any{
		"last_name": "Smith",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrValidation))
}

func TestCreateEnumInvalid(t *testing.T) {
	s, _ := newGenericStore(t)
	_, err := s.Create(context.Background(), "deal", map[string]any{
		"title": "Acme",
		"stage": "closed",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrValidation))
}

func TestCreateAppliesEnumDefault(t *testing.T) {
	s, _ := newGenericStore(t)
	rec, err := s.Create(context.Background(), "deal", map[string]any{
		"title": "Acme",
	})
	require.NoError(t, err)
	assert.Equal(t, "lead", rec["stage"])
}

func TestCreateUniqueCollision(t *testing.T) {
	s, _ := newGenericStore(t)
	ctx := context.Background()
	_, err := s.Create(ctx, "person", map[string]any{
		"first_name": "Jane",
		"email":      "dup@example.com",
	})
	require.NoError(t, err)

	_, err = s.Create(ctx, "person", map[string]any{
		"first_name": "John",
		"email":      "dup@example.com",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrConflict))
}

func TestCreateSlugSuffixOnNameCollision(t *testing.T) {
	s, _ := newGenericStore(t)
	ctx := context.Background()
	r1, err := s.Create(ctx, "person", map[string]any{
		"first_name": "Jane", "last_name": "Smith",
	})
	require.NoError(t, err)
	r2, err := s.Create(ctx, "person", map[string]any{
		"first_name": "Jane", "last_name": "Smith",
	})
	require.NoError(t, err)
	assert.Equal(t, "jane-smith", r1["id"])
	assert.Equal(t, "jane-smith-2", r2["id"])
}

func TestGetAndList(t *testing.T) {
	s, _ := newGenericStore(t)
	ctx := context.Background()
	for _, name := range []string{"Ana Zed", "Jane Smith", "Bob Roe"} {
		parts := splitName(name)
		_, err := s.Create(ctx, "person", map[string]any{
			"first_name": parts[0], "last_name": parts[1],
		})
		require.NoError(t, err)
	}
	rec, err := s.Get(ctx, "person", "jane-smith")
	require.NoError(t, err)
	assert.Equal(t, "Jane", rec["first_name"])

	listed, err := s.List(ctx, "person", nil)
	require.NoError(t, err)
	require.Len(t, listed, 3)
	assert.Equal(t, "ana-zed", listed[0]["id"])
	assert.Equal(t, "bob-roe", listed[1]["id"])
	assert.Equal(t, "jane-smith", listed[2]["id"])
}

func TestListTagFilter(t *testing.T) {
	s, _ := newGenericStore(t)
	ctx := context.Background()
	_, err := s.Create(ctx, "person", map[string]any{
		"first_name": "Jane", "tags": []string{"vip", "eu"},
	})
	require.NoError(t, err)
	_, err = s.Create(ctx, "person", map[string]any{
		"first_name": "Bob", "tags": []string{"eu"},
	})
	require.NoError(t, err)

	vips, err := s.List(ctx, "person", map[string]any{"tag": "vip"})
	require.NoError(t, err)
	require.Len(t, vips, 1)
	assert.Equal(t, "jane", vips[0]["id"])
}

func TestUpdatePatchesAndBumpsTimestamp(t *testing.T) {
	s, idx := newGenericStore(t)
	ctx := context.Background()
	withFixedTimeSeq(t, []string{"2026-04-17T00:00:00Z", "2026-04-17T00:00:01Z"})

	_, err := s.Create(ctx, "person", map[string]any{"first_name": "Jane"})
	require.NoError(t, err)

	rec, err := s.Update(ctx, "person", "jane", map[string]any{"email": "j@x"})
	require.NoError(t, err)
	assert.Equal(t, "j@x", rec["email"])
	assert.NotEqual(t, rec["created_at"], rec["updated_at"])

	// Two upserts: Create + Update.
	assert.Len(t, idx.upserts, 2)
}

func TestUpdateUniqueConflict(t *testing.T) {
	s, _ := newGenericStore(t)
	ctx := context.Background()
	_, err := s.Create(ctx, "person", map[string]any{
		"first_name": "Jane", "email": "a@x",
	})
	require.NoError(t, err)
	_, err = s.Create(ctx, "person", map[string]any{
		"first_name": "Bob", "email": "b@x",
	})
	require.NoError(t, err)

	_, err = s.Update(ctx, "person", "bob", map[string]any{"email": "a@x"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrConflict))
}

func TestArchiveEntityRemovesFromIndex(t *testing.T) {
	s, idx := newGenericStore(t)
	ctx := context.Background()
	_, err := s.Create(ctx, "person", map[string]any{"first_name": "Jane"})
	require.NoError(t, err)

	require.NoError(t, s.ArchiveEntity(ctx, "person", "jane"))
	_, err = os.Stat(filepath.Join(s.Root(), "people", "jane.md"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(s.Root(), s.Schema().Store.ArchiveDir, "people", "jane.md"))
	assert.NoError(t, err)
	assert.Equal(t, []string{"person:jane"}, idx.removed)
}

func TestTagWorkflow(t *testing.T) {
	s, _ := newGenericStore(t)
	ctx := context.Background()
	_, err := s.Create(ctx, "person", map[string]any{"first_name": "Jane"})
	require.NoError(t, err)
	_, err = s.Create(ctx, "person", map[string]any{"first_name": "Bob"})
	require.NoError(t, err)

	require.NoError(t, s.AddTag(ctx, "person", "jane", "vip"))
	require.NoError(t, s.AddTag(ctx, "person", "bob", "vip"))
	require.NoError(t, s.AddTag(ctx, "person", "jane", "eu"))

	tags, err := s.TagsFor(ctx, "person", "jane")
	require.NoError(t, err)
	assert.Equal(t, []string{"eu", "vip"}, tags)

	counts, err := s.ListTags(ctx)
	require.NoError(t, err)
	require.Len(t, counts, 2)
	assert.Equal(t, "eu", counts[0].Name)
	assert.Equal(t, 1, counts[0].Count)
	assert.Equal(t, "vip", counts[1].Name)
	assert.Equal(t, 2, counts[1].Count)

	count, err := s.CountTagUsage(ctx, "vip")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	modified, err := s.DeleteTag(ctx, "vip")
	require.NoError(t, err)
	assert.Equal(t, 2, modified)

	count, err = s.CountTagUsage(ctx, "vip")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestReindexCountsAll(t *testing.T) {
	s, idx := newGenericStore(t)
	ctx := context.Background()
	_, err := s.Create(ctx, "person", map[string]any{"first_name": "Jane"})
	require.NoError(t, err)
	_, err = s.Create(ctx, "deal", map[string]any{"title": "Acme"})
	require.NoError(t, err)

	idx.upserts = nil // reset; Reindex should re-emit every record.
	stats, err := s.Reindex(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.ByKind["person"])
	assert.Equal(t, 1, stats.ByKind["deal"])
	assert.Equal(t, 0, stats.ByKind["organization"])
	assert.Len(t, idx.upserts, 2)
}

// withFixedTimeSeq replaces nowRFC3339 with a sequence returning each
// entry in order. Restores the original on test cleanup.
func withFixedTimeSeq(t *testing.T, seq []string) {
	t.Helper()
	orig := nowRFC3339
	i := 0
	nowRFC3339 = func() string {
		v := seq[i%len(seq)]
		i++
		return v
	}
	t.Cleanup(func() { nowRFC3339 = orig })
}

func splitName(full string) []string {
	for i := 0; i < len(full); i++ {
		if full[i] == ' ' {
			return []string{full[:i], full[i+1:]}
		}
	}
	return []string{full, ""}
}
