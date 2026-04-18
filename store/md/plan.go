package md

// WritePlan is the agent-facing description of a mutation — what a
// write *would* do (dry-run) or did do. Used by the runtime to render
// --dry-run output and to build golden pairs that match the executed
// write byte-for-byte.
//
// Path and ToPath are root-relative so goldens remain stable across
// temp-dir runs.
type WritePlan struct {
	Action      string         `json:"action"` // create|update|archive|delete
	Kind        string         `json:"kind"`
	Slug        string         `json:"slug"`
	ParentKind  string         `json:"parent_kind,omitempty"`
	ParentSlug  string         `json:"parent_slug,omitempty"`
	Path        string         `json:"path"`
	ToPath      string         `json:"to_path,omitempty"` // archive only — destination path
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Body        string         `json:"body,omitempty"`
	Before      *PlanBefore    `json:"before,omitempty"` // update|archive|delete only
}

// PlanBefore captures the pre-mutation state of an entity or sub-file.
// Populated for update/archive/delete so the diff between before and
// {frontmatter, body} is apparent in one payload.
type PlanBefore struct {
	Frontmatter map[string]any `json:"frontmatter"`
	Body        string         `json:"body"`
}
