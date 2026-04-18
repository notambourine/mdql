package md

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlanCreateNoWrites asserts the core dry-run promise: PlanCreate
// validates and computes the write plan without touching the disk or
// the indexer.
func TestPlanCreateNoWrites(t *testing.T) {
	s, idx := newGenericStore(t)
	plan, err := s.PlanCreate(context.Background(), "person", map[string]any{
		"first_name": "Jane",
		"last_name":  "Smith",
		"body":       "# Notes",
	})
	require.NoError(t, err)
	assert.Equal(t, "create", plan.Action)
	assert.Equal(t, "person", plan.Kind)
	assert.Equal(t, "jane-smith", plan.Slug)
	assert.Equal(t, filepath.Join("people", "jane-smith", "index.md"), plan.Path)
	assert.Equal(t, "Jane", plan.Frontmatter["first_name"])
	assert.Equal(t, "# Notes", plan.Body)
	assert.Nil(t, plan.Before)

	_, err = os.Stat(filepath.Join(s.Root(), "people", "jane-smith"))
	assert.True(t, os.IsNotExist(err), "plan must not create the folder")
	assert.Empty(t, idx.upserts, "plan must not touch the indexer")
}

// TestPlanCreateValidates exercises the validation path — dry-run
// surfaces required-field errors before anything reaches the disk.
func TestPlanCreateValidates(t *testing.T) {
	s, _ := newGenericStore(t)
	_, err := s.PlanCreate(context.Background(), "person", map[string]any{})
	require.Error(t, err)
}

// TestPlanUpdateIncludesBefore confirms the update plan carries a
// before/after snapshot so diffs are visible in one payload.
func TestPlanUpdateIncludesBefore(t *testing.T) {
	s, idx := newGenericStore(t)
	_, err := s.Create(context.Background(), "person", map[string]any{
		"first_name": "Jane",
		"email":      "jane@x",
	})
	require.NoError(t, err)
	idx.upserts = nil

	plan, err := s.PlanUpdate(context.Background(), "person", "jane", map[string]any{
		"email": "jane@y",
	})
	require.NoError(t, err)
	assert.Equal(t, "update", plan.Action)
	assert.Equal(t, "jane", plan.Slug)
	require.NotNil(t, plan.Before)
	assert.Equal(t, "jane@x", plan.Before.Frontmatter["email"])
	assert.Equal(t, "jane@y", plan.Frontmatter["email"])
	assert.Empty(t, idx.upserts, "plan update must not reindex")

	// disk unchanged
	on, err := s.Get(context.Background(), "person", "jane")
	require.NoError(t, err)
	assert.Equal(t, "jane@x", on["email"])
}

// TestPlanArchiveEntityIncludesSubDeindex asserts the archive plan
// captures the source/destination paths without invoking any file
// move or index removal, even when the parent has sub-files.
func TestPlanArchiveEntityIncludesSubDeindex(t *testing.T) {
	s := newSubFileStore(t)
	_, err := s.CreateSubFile(context.Background(), "project", "launch-site", "meeting", map[string]any{
		"subject": "kickoff",
		"date":    "2026-04-17",
	})
	require.NoError(t, err)

	plan, err := s.PlanArchiveEntity(context.Background(), "project", "launch-site")
	require.NoError(t, err)
	assert.Equal(t, "archive", plan.Action)
	assert.Equal(t, "launch-site", plan.Slug)
	assert.Equal(t, filepath.Join("projects", "launch-site"), plan.Path)
	assert.Contains(t, plan.ToPath, filepath.Join("projects", "launch-site"))
	require.NotNil(t, plan.Before)
	assert.Equal(t, "Launch Site", plan.Before.Frontmatter["name"])

	// source folder and sub-files must still be intact.
	_, err = os.Stat(filepath.Join(s.Root(), "projects", "launch-site", "meetings", "2026-04-17-kickoff.md"))
	require.NoError(t, err, "sub-file should still exist after plan")
}

// TestPlanSubFileLifecycle walks the create/update/delete dry-run path
// to confirm each sibling method returns a populated plan and leaves
// disk state unchanged.
func TestPlanSubFileLifecycle(t *testing.T) {
	s := newSubFileStore(t)
	ctx := context.Background()

	plan, err := s.PlanCreateSubFile(ctx, "project", "launch-site", "meeting", map[string]any{
		"subject": "kickoff",
		"date":    "2026-04-17",
	})
	require.NoError(t, err)
	assert.Equal(t, "create", plan.Action)
	assert.Equal(t, "meeting", plan.Kind)
	assert.Equal(t, "project", plan.ParentKind)
	assert.Equal(t, "2026-04-17-kickoff", plan.Slug)
	_, err = os.Stat(filepath.Join(s.Root(), "projects", "launch-site", "meetings", "2026-04-17-kickoff.md"))
	assert.True(t, os.IsNotExist(err), "plan must not create the sub-file")

	// Actually create one to test update/delete plans.
	_, err = s.CreateSubFile(ctx, "project", "launch-site", "meeting", map[string]any{
		"subject": "kickoff",
		"date":    "2026-04-17",
	})
	require.NoError(t, err)

	uplan, err := s.PlanUpdateSubFile(ctx, "project", "launch-site", "meeting", "2026-04-17-kickoff", map[string]any{
		"subject": "kickoff-v2",
	})
	require.NoError(t, err)
	assert.Equal(t, "update", uplan.Action)
	require.NotNil(t, uplan.Before)
	assert.Equal(t, "kickoff", uplan.Before.Frontmatter["subject"])
	assert.Equal(t, "kickoff-v2", uplan.Frontmatter["subject"])

	dplan, err := s.PlanDeleteSubFile(ctx, "project", "launch-site", "meeting", "2026-04-17-kickoff")
	require.NoError(t, err)
	assert.Equal(t, "delete", dplan.Action)
	require.NotNil(t, dplan.Before)
	assert.Equal(t, "kickoff", dplan.Before.Frontmatter["subject"])

	// sub-file still on disk after both plans.
	_, err = os.Stat(filepath.Join(s.Root(), "projects", "launch-site", "meetings", "2026-04-17-kickoff.md"))
	require.NoError(t, err)
}
