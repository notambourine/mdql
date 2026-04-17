package md

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/notambourine/mdql/schema"
)

// fixtureSchemaYAML is the minimal CRM-shaped schema used by md/ unit
// tests. Exercises at least one entity with a non-trivial slug template
// and one with append_only semantics so that tests cover both branches
// of runtime behavior (archivable vs not).
const fixtureSchemaYAML = `
version: 1
entities:
  person:
    dir: people
    title: "{{.first_name}} {{.last_name}}"
    slug:  "{{.first_name}} {{.last_name}}"
    fields:
      first_name: {type: string, required: true}
      last_name:  {type: string}
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
  task:
    dir: tasks
    title: "{{.title}}"
    slug:  "{{.title}}"
    fields:
      title: {type: string, required: true}
  interaction:
    dir: interactions
    append_only: true
    title: "{{.subject}}"
    slug:  "{{.subject}}"
    fields:
      subject: {type: string, required: true}
`

// fixtureSchema parses fixtureSchemaYAML. Call per-test to get a
// fresh *Schema (cheap; Parse allocates a few maps).
func fixtureSchema(t *testing.T) *schema.Schema {
	t.Helper()
	return mustParseSchema(t, fixtureSchemaYAML)
}

// mustParseSchema parses yml, failing the test on error. Shared by
// tests that need a different fixture than fixtureSchemaYAML.
func mustParseSchema(t *testing.T, yml string) *schema.Schema {
	t.Helper()
	s, err := schema.Parse([]byte(yml))
	require.NoError(t, err)
	return s
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	sch := fixtureSchema(t)
	require.NoError(t, Init(root, sch))
	s, err := Open(root, sch, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// flatFixtureYAML has one entity declared `flat: true` so path-assertion
// tests can exercise the sprawl-opt-out branch.
const flatFixtureYAML = `
version: 1
entities:
  tag:
    dir: tags
    flat: true
    title: "{{.name}}"
    slug:  "{{.name}}"
    fields:
      name: {type: string, required: true}
`

func newFlatTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	sch := mustParseSchema(t, flatFixtureYAML)
	require.NoError(t, Init(root, sch))
	s, err := Open(root, sch, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}
