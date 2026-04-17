package schema

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const crmFixture = `
version: 1
store:
  runtime_dir: ".crm"
  archive_dir: "_archive"
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
      relationships:
        type: "relation[]"
        target: person
        edge_types: [colleague, friend, mentor]
  organization:
    dir: organizations
    title: "{{.name}}"
    slug: "{{.name}}"
    fields:
      name:   {type: string, required: true}
      domain: {type: string}
  deal:
    dir: deals
    title: "{{.title}}"
    slug:  "{{.title}}"
    fields:
      title: {type: string, required: true}
      value: {type: float}
      stage: {type: enum, values: [lead, won, lost], required: true, default: lead}
      org:   {type: link, target: organization}
  task:
    dir: tasks
    title: "{{.title}}"
    slug:  '{{.due_at | date "2006-01-02"}}-{{.title}}'
    fields:
      title:     {type: string, required: true}
      due_at:    {type: date}
      priority:  {type: enum, values: [low, medium, high], default: medium}
      completed: {type: bool, default: false}
  interaction:
    dir: interactions
    append_only: true
    archivable: false
    title: "{{.type}} · {{.subject}}"
    slug:  '{{.occurred_at | date "2006-01-02T150405Z"}}-{{.type}}'
    fields:
      type:        {type: enum, values: [call, email, meeting], required: true}
      subject:     {type: string}
      occurred_at: {type: date, required: true}
      people:      {type: "link[]", target: person}
`

func TestParseCRMFixture(t *testing.T) {
	s, err := Parse([]byte(crmFixture))
	require.NoError(t, err)
	assert.Equal(t, 1, s.Version)
	assert.Equal(t, ".crm", s.Store.RuntimeDir)
	assert.Equal(t, "_archive", s.Store.ArchiveDir)
	assert.Len(t, s.Entities, 5)

	person := s.Entities["person"]
	assert.Equal(t, "people", person.Dir)
	assert.True(t, person.IsArchivable(), "archivable defaults to true when unset")
	assert.True(t, person.Fields["first_name"].Required)
	assert.True(t, person.Fields["email"].Unique)
	assert.True(t, person.Fields["tags"].Sorted)
	assert.Equal(t, "string[]", person.Fields["tags"].Type)

	rel := person.Fields["relationships"]
	assert.Equal(t, "relation[]", rel.Type)
	assert.Equal(t, "person", rel.Target)
	assert.Equal(t, []string{"colleague", "friend", "mentor"}, rel.EdgeTypes)

	interaction := s.Entities["interaction"]
	assert.True(t, interaction.AppendOnly)
	assert.False(t, interaction.IsArchivable(), "explicit archivable: false survives")

	deal := s.Entities["deal"]
	stage := deal.Fields["stage"]
	assert.Equal(t, "enum", stage.Type)
	assert.Equal(t, []string{"lead", "won", "lost"}, stage.Values)
	assert.Equal(t, "lead", stage.Default)
}

func TestDefaultsApplied(t *testing.T) {
	yml := `
version: 1
entities:
  person:
    dir: people
    title: "{{.name}}"
    slug: "{{.name}}"
    fields:
      name: {type: string, required: true}
`
	s, err := Parse([]byte(yml))
	require.NoError(t, err)
	assert.Equal(t, ".mdql", s.Store.RuntimeDir)
	assert.Equal(t, "_archive", s.Store.ArchiveDir)
	assert.True(t, s.Entities["person"].IsArchivable())
}

func TestValidateErrors(t *testing.T) {
	cases := []struct {
		name    string
		yml     string
		wantMsg string
	}{
		{
			name: "unsupported version",
			yml: `
version: 2
entities:
  person: {dir: p, title: t, slug: s, fields: {n: {type: string}}}`,
			wantMsg: "unsupported version",
		},
		{
			name: "no entities",
			yml: `
version: 1
entities: {}`,
			wantMsg: "no entities",
		},
		{
			name: "missing dir",
			yml: `
version: 1
entities:
  person: {title: t, slug: s, fields: {n: {type: string}}}`,
			wantMsg: "dir is required",
		},
		{
			name: "unknown field type",
			yml: `
version: 1
entities:
  person:
    dir: p
    title: t
    slug: s
    fields:
      shape: {type: polygon}`,
			wantMsg: `unknown type "polygon"`,
		},
		{
			name: "link without target",
			yml: `
version: 1
entities:
  person:
    dir: p
    title: t
    slug: s
    fields:
      org: {type: link}`,
			wantMsg: "link requires target",
		},
		{
			name: "link target not in schema",
			yml: `
version: 1
entities:
  person:
    dir: p
    title: t
    slug: s
    fields:
      org: {type: link, target: organization}`,
			wantMsg: `target "organization" not defined`,
		},
		{
			name: "enum missing values",
			yml: `
version: 1
entities:
  person:
    dir: p
    title: t
    slug: s
    fields:
      color: {type: enum}`,
			wantMsg: "enum requires values",
		},
		{
			name: "relation missing edge_types",
			yml: `
version: 1
entities:
  person:
    dir: p
    title: t
    slug: s
    fields:
      peers: {type: "relation[]", target: person}`,
			wantMsg: "requires edge_types",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yml))
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.wantMsg),
				"err=%q want substring %q", err.Error(), tc.wantMsg)
		})
	}
}

func TestRenderTitleAndSlug(t *testing.T) {
	title, err := Render("{{.first_name}} {{.last_name}}", map[string]any{
		"first_name": "Jane",
		"last_name":  "Smith",
	})
	require.NoError(t, err)
	assert.Equal(t, "Jane Smith", title)

	due := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	slug, err := Render(`{{.due_at | date "2006-01-02"}}-{{.title}}`, map[string]any{
		"due_at": due,
		"title":  "ship-it",
	})
	require.NoError(t, err)
	assert.Equal(t, "2026-04-17-ship-it", slug)

	slugStr, err := Render(`{{.due_at | date "2006-01-02"}}-{{.title}}`, map[string]any{
		"due_at": "2026-04-17",
		"title":  "string-date",
	})
	require.NoError(t, err)
	assert.Equal(t, "2026-04-17-string-date", slugStr)

	idx, err := Render(`{{index .people 0}}`, map[string]any{
		"people": []any{"jane-smith", "bob"},
	})
	require.NoError(t, err)
	assert.Equal(t, "jane-smith", idx)
}

func TestRenderBadTemplate(t *testing.T) {
	_, err := Render("{{.unterminated", map[string]any{})
	require.Error(t, err)
}
