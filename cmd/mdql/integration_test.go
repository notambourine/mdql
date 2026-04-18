package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fixtureSchema is the canonical two-entity schema the integration
// tests run against: person (flat fields) and project (link + enum +
// four sub-file kinds covering dir/slug templating, required/default
// validation, and catchall absorption). Deliberately minimal — every
// feature the CLI exposes is exercised by at least one test, and
// nothing more. Replaces the previous CRM-shaped fixture (person/
// organization/deal/task) per commit-3 spec: one fixture covers
// entity CRUD, sub-file CRUD, and the schema describe contract.
const fixtureSchema = `
version: 1
store:
  runtime_dir: ".mdql"
  archive_dir: "_archive"
entities:
  person:
    dir: people
    title: "{{.name}}"
    slug:  "{{.name}}"
    fields:
      name:  {type: string, required: true}
      email: {type: string, unique: true}
      role:  {type: enum, values: [ic, lead, exec], required: true, default: ic}
      tags:  {type: "string[]", sorted: true}
  project:
    dir: projects
    title: "{{.name}}"
    slug:  "{{.name}}"
    fields:
      name:  {type: string, required: true}
      stage: {type: enum, values: [draft, active, done], required: true, default: draft}
      lead:  {type: link, target: person}
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
          title:      {type: string, required: true}
          supersedes: {type: link, target: project.decision}
      milestone:
        dir: milestones
        slug: "{{.name}}"
        fields:
          name: {type: string, required: true}
          due:  {type: string}
      note:
        catchall: true
        dir: notes
        fields:
          title: {type: string}
`

var mdqlBinary string

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "mdql-integration-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdir temp:", err)
		return 1
	}
	defer os.RemoveAll(tmp)

	mdqlBinary = filepath.Join(tmp, "mdql")
	build := exec.Command("go", "build", "-o", mdqlBinary, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build mdql:", err)
		return 1
	}
	return m.Run()
}

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// run invokes the built mdql binary against root with args appended
// after --root/--schema. Returns the exit code rather than failing so
// tests can assert non-zero classifications (ErrValidation=2, etc.).
func run(t *testing.T, root string, args ...string) runResult {
	t.Helper()
	full := append(
		[]string{"--root", root, "--schema", filepath.Join(root, "schema.yml")},
		args...,
	)
	cmd := exec.Command(mdqlBinary, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return runResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func mustRun(t *testing.T, root string, args ...string) runResult {
	t.Helper()
	r := run(t, root, args...)
	if r.exitCode != 0 {
		t.Fatalf("mdql %v: exit %d\nstdout: %s\nstderr: %s",
			args, r.exitCode, r.stdout, r.stderr)
	}
	return r
}

func initStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "schema.yml"), []byte(fixtureSchema), 0o644); err != nil {
		t.Fatalf("write schema.yml: %v", err)
	}
	mustRun(t, root, "init")
	return root
}

// decodeJSON unmarshals r.stdout into []map[string]any. Fatals on parse
// failure so call sites stay terse.
func decodeJSON(t *testing.T, r runResult) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("parse json: %v\nstdout: %s", err, r.stdout)
	}
	return out
}

// decodeJSONObject unmarshals r.stdout into a single map. Used for
// commands that emit one record (show).
func decodeJSONObject(t *testing.T, r runResult) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("parse json: %v\nstdout: %s", err, r.stdout)
	}
	return out
}

// scrub removes per-run variable fields so a record can be compared to a
// golden shape. body is absent from list output but can leak in when a
// create returns; auto-injected uuid/created_at/updated_at no longer
// exist (filename is canonical id), so this is a no-op for those keys.
func scrub(recs []map[string]any) []map[string]any {
	for _, r := range recs {
		delete(r, "body")
	}
	return recs
}

func TestPersonRoundtrip(t *testing.T) {
	root := initStore(t)

	add := mustRun(t, root, "--format", "json", "person", "add",
		"--name", "Jane Smith",
		"--email", "jane@example.com")
	added := decodeJSON(t, add)
	if len(added) != 1 {
		t.Fatalf("expected 1 record, got %d", len(added))
	}
	if added[0]["id"] != "jane-smith" {
		t.Errorf("slug = %v, want jane-smith", added[0]["id"])
	}

	folder := filepath.Join(root, "people", "jane-smith")
	path := filepath.Join(folder, "index.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("index.md not written: %v", err)
	}

	mustRun(t, root, "person", "update", "jane-smith", "--email", "jane@new.example.com")

	show := mustRun(t, root, "--format", "json", "person", "show", "jane-smith")
	shown := decodeJSONObject(t, show)
	if shown["email"] != "jane@new.example.com" {
		t.Errorf("email not updated: %v", shown["email"])
	}

	mustRun(t, root, "person", "archive", "jane-smith")
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Errorf("sprawl folder still exists after archive: %v", err)
	}
	archived := filepath.Join(root, "_archive", "people", "jane-smith", "index.md")
	if _, err := os.Stat(archived); err != nil {
		t.Errorf("archived index.md missing: %v", err)
	}
}

func TestProjectRoundtrip(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site", "--stage", "active")
	list := mustRun(t, root, "--format", "json", "project", "list")
	projects := decodeJSON(t, list)
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	id := projects[0]["id"].(string)
	mustRun(t, root, "project", "update", id, "--stage", "done")
	show := mustRun(t, root, "--format", "json", "project", "show", id)
	shown := decodeJSONObject(t, show)
	if shown["stage"] != "done" {
		t.Errorf("stage after update = %v, want done", shown["stage"])
	}
	mustRun(t, root, "project", "archive", id)
}

// TestListGoldenShape asserts `person list --format json` matches the
// expected shape after two inserts. scrub() drops the body field which
// can leak in from create-return paths.
func TestListGoldenShape(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Ana Zed", "--email", "ana@x")
	mustRun(t, root, "person", "add", "--name", "Bob Roe", "--email", "bob@x")

	list := mustRun(t, root, "--format", "json", "person", "list")
	got := scrub(decodeJSON(t, list))

	want := []map[string]any{
		{"id": "ana-zed", "name": "Ana Zed", "email": "ana@x"},
		{"id": "bob-roe", "name": "Bob Roe", "email": "bob@x"},
	}
	assertGoldenEqual(t, want, got)
}

// TestSlugCollisionSuffixesFilename pins the merge-collision behavior:
// distinct records with identical slug inputs must get suffixed slugs
// (filename-as-canonical-id). The previous version of this test also
// asserted distinct UUIDs, but UUIDs were dropped — filename suffix is
// now the only identity disambiguator.
func TestSlugCollisionSuffixesFilename(t *testing.T) {
	root := initStore(t)
	a := decodeJSON(t, mustRun(t, root, "--format", "json", "person", "add",
		"--name", "Jane Smith", "--email", "jane1@x"))
	b := decodeJSON(t, mustRun(t, root, "--format", "json", "person", "add",
		"--name", "Jane Smith", "--email", "jane2@x"))

	if a[0]["id"] != "jane-smith" {
		t.Errorf("first slug = %v, want jane-smith", a[0]["id"])
	}
	if b[0]["id"] != "jane-smith-2" {
		t.Errorf("collision slug = %v, want jane-smith-2", b[0]["id"])
	}
}

// TestWikiLinksAliasToFirstSlug exercises the semantic-drift limitation
// pinned in CLAUDE.md: once two "Jane Smith" records exist, a body
// `[[jane-smith]]` reference only resolves to record A. wiki.Check
// sees the target slug exists so reports no dangling — the drift is
// invisible by design.
func TestWikiLinksAliasToFirstSlug(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "a@x")
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "b@x")

	mustRun(t, root, "project", "add",
		"--name", "Relate",
		"--body", "Spoke with [[jane-smith]]; follow-up owed to [[jane-smith]].",
	)

	dangling := mustRun(t, root, "--format", "json", "wiki", "dangling").stdout
	dangling = strings.TrimSpace(dangling)
	if dangling != "null" && dangling != "[]" {
		t.Errorf("expected empty dangling (semantic drift is invisible), got: %s", dangling)
	}
}

// TestRenameSurfacesDanglingLinks follows on from the alias case: if
// the target file is renamed (and its frontmatter id rewritten) the
// same [[jane-smith]] reference is now broken, and wiki.Check must
// surface it.
func TestRenameSurfacesDanglingLinks(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "a@x")
	mustRun(t, root, "project", "add",
		"--name", "Relate",
		"--body", "Ref [[jane-smith]].",
	)

	// Rename the sprawl folder. mdql has no rename command — we emulate
	// the out-of-band edit that any real user would do (e.g. git mv),
	// then confirm the wiki check surfaces the now-broken reference.
	oldFolder := filepath.Join(root, "people", "jane-smith")
	newFolder := filepath.Join(root, "people", "jane-smith-renamed")
	if err := os.Rename(oldFolder, newFolder); err != nil {
		t.Fatalf("rename sprawl folder: %v", err)
	}

	dangling := mustRun(t, root, "--format", "json", "wiki", "dangling").stdout
	if !strings.Contains(dangling, `"target_slug":"jane-smith"`) {
		t.Errorf("expected dangling ref to jane-smith, got: %s", dangling)
	}
}

func TestFormatParity(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "jane@example.com")
	mustRun(t, root, "person", "add", "--name", "Bob Jones", "--email", "bob@example.com")

	jsonOut := mustRun(t, root, "--format", "json", "person", "list")
	if len(decodeJSON(t, jsonOut)) != 2 {
		t.Fatalf("json list len != 2\nstdout: %s", jsonOut.stdout)
	}

	csvOut := mustRun(t, root, "--format", "csv", "person", "list")
	csvLines := strings.Split(strings.TrimRight(csvOut.stdout, "\n"), "\n")
	if len(csvLines) != 3 {
		t.Fatalf("csv lines = %d, want 3 (header + 2 rows)\nstdout: %s", len(csvLines), csvOut.stdout)
	}
	if !strings.Contains(csvLines[0], "id") {
		t.Errorf("csv header missing id column: %q", csvLines[0])
	}

	tsvOut := mustRun(t, root, "--format", "tsv", "person", "list")
	tsvLines := strings.Split(strings.TrimRight(tsvOut.stdout, "\n"), "\n")
	if len(tsvLines) != 3 {
		t.Fatalf("tsv lines = %d, want 3", len(tsvLines))
	}
	if !strings.Contains(tsvLines[0], "\t") {
		t.Errorf("tsv header lacks tab separator: %q", tsvLines[0])
	}
}

func TestQuietEmitsIDsOnly(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "j@x")
	mustRun(t, root, "person", "add", "--name", "Bob Jones", "--email", "b@x")

	r := mustRun(t, root, "--quiet", "person", "list")
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	want := map[string]bool{"jane-smith": true, "bob-jones": true}
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), r.stdout)
	}
	for _, line := range lines {
		if !want[line] {
			t.Errorf("unexpected line %q", line)
		}
	}
}

func TestUnknownSlugExit3(t *testing.T) {
	root := initStore(t)
	r := run(t, root, "person", "show", "nobody-here")
	if r.exitCode != 3 {
		t.Errorf("exit code = %d, want 3 (ErrNotFound)\nstderr: %s", r.exitCode, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("expected empty stdout on error, got: %q", r.stdout)
	}
}

// TestOrphanTitlePopulated pins the fix for the empty-title bug on
// `wiki orphans`: a lonely record must surface with its rendered
// title, matching the behavior of `wiki backlinks` (which sources
// titles from the search index). Before the fix, wiki.Orphans
// constructed Ref{Type, Slug} with no Title at all.
func TestOrphanTitlePopulated(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Lonely Soul", "--email", "l@x")
	r := mustRun(t, root, "--format", "json", "wiki", "orphans").stdout
	if !strings.Contains(r, `"title":"Lonely Soul"`) {
		t.Errorf("expected orphan title 'Lonely Soul', got: %s", r)
	}
}

// TestFrontmatterLinkBacklinks pins the fix for the frontmatter-link
// indexing bug: a link field written to disk as "[[slug]]" (the
// documented wiki form, and what yaml.Marshal produces when the CLI
// arg is bracket-wrapped) must still surface via `wiki backlinks`.
//
// Before the fix, entityDoc pushed the raw "[[slug]]" string into the
// bleve links_to field — so Backlinks on the target missed the
// bracket-wrapped entry. The contradicted doc claim is wiki/wiki.go:30
// ("frontmatter or body").
func TestFrontmatterLinkBacklinks(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "jane@x")
	// Pass the wiki-wrapped form at the CLI — simulates a user who
	// writes the frontmatter-native syntax and also matches the shape
	// seeded by example/run.sh.
	mustRun(t, root, "project", "add",
		"--name", "Launch Site",
		"--lead", "[[jane-smith]]",
	)
	mustRun(t, root, "index", "rebuild")
	backlinks := mustRun(t, root, "--format", "json", "wiki", "backlinks", "jane-smith").stdout
	if !strings.Contains(backlinks, `"slug":"launch-site"`) {
		t.Errorf("expected launch-site in backlinks, got: %s", backlinks)
	}
}

// TestRequiredFieldWithDefaultSkipsFlagValidation pins the fix for
// the required+default footgun: project.stage is {required: true,
// default: draft}, so `project add --name X` (no --stage) must
// succeed and the schema default must land in the record. Before the
// fix, cobra rejected the command at flag-parse time with "required
// flag(s) stage not set" — defaults are applied later inside
// store.Create, so requiring the flag made the default unreachable.
func TestRequiredFieldWithDefaultSkipsFlagValidation(t *testing.T) {
	root := initStore(t)
	r := run(t, root, "--format", "json", "project", "add", "--name", "Default Stage Project")
	if r.exitCode != 0 {
		t.Fatalf("project add without --stage: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	added := decodeJSON(t, r)
	if added[0]["stage"] != "draft" {
		t.Errorf("stage = %v, want draft (schema default)", added[0]["stage"])
	}
}

func TestValidationExit2(t *testing.T) {
	root := initStore(t)
	r := run(t, root, "project", "add", "--name", "Bad Project", "--stage", "not-a-stage")
	if r.exitCode != 2 {
		t.Errorf("exit code = %d, want 2 (ErrValidation)\nstderr: %s", r.exitCode, r.stderr)
	}
}

// TestDroppedCommandsAbsent pins the intentional parity gap with
// crm-cli: status, context, and log were removed during the
// extraction. Scans --help text rather than running the commands —
// cobra's default for an unknown subcommand under a parent with no
// Run is "show help, exit 0", so exit-code probing is unreliable.
func TestDroppedCommandsAbsent(t *testing.T) {
	root := initStore(t)
	rootHelp := mustRun(t, root, "--help").stdout
	for _, cmd := range []string{"status", "context", "log"} {
		if strings.Contains(rootHelp, "\n  "+cmd+" ") {
			t.Errorf("dropped root command %q present in --help:\n%s", cmd, rootHelp)
		}
	}
}

// TestSchemaFlagInHelp guards the cosmetic fix that advertises the
// pre-cobra --schema flag in `mdql --help`. The flag is authoritatively
// parsed in main() before cobra runs (the schema shapes the command
// tree), then re-declared on the cobra root purely for discoverability.
func TestSchemaFlagInHelp(t *testing.T) {
	root := initStore(t)
	help := mustRun(t, root, "--help").stdout
	if !strings.Contains(help, "--schema") {
		t.Errorf("--schema not advertised in --help:\n%s", help)
	}
}

// TestSchemaDescribeIsAgentLoadable pins the contract that
// `mdql schema describe` returns a JSON view containing every entity,
// its frontmatter fields, and the commands an agent can derive from it.
// This is the canonical session-entry payload — agents use it to learn
// what mdql supports without having to read schema.yml directly.
func TestSchemaDescribeIsAgentLoadable(t *testing.T) {
	root := initStore(t)
	r := mustRun(t, root, "schema", "describe")

	var desc map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &desc); err != nil {
		t.Fatalf("parse schema describe: %v\nstdout: %s", err, r.stdout)
	}
	entities, ok := desc["entities"].(map[string]any)
	if !ok {
		t.Fatalf("entities key missing or wrong type: %v", desc)
	}
	for _, want := range []string{"person", "project"} {
		if _, ok := entities[want]; !ok {
			t.Errorf("entity %q missing from describe output", want)
		}
	}
	person, ok := entities["person"].(map[string]any)
	if !ok {
		t.Fatalf("person not an object")
	}
	fields, ok := person["frontmatter_fields"].(map[string]any)
	if !ok || fields["name"] == nil {
		t.Errorf("person.frontmatter_fields.name missing: %v", person)
	}
	if _, ok := person["sub_files"].(map[string]any); !ok {
		t.Errorf("person.sub_files missing (should be empty object for entities without files): %v", person)
	}
	cmds, ok := person["commands"].([]any)
	if !ok || len(cmds) == 0 {
		t.Errorf("person.commands missing or empty: %v", person)
	}
	store, ok := desc["store"].(map[string]any)
	if !ok || store["runtime_dir"] != ".mdql" {
		t.Errorf("store.runtime_dir wrong: %v", store)
	}
}

// TestWikiDanglingReturnsEmptyArrayNotNull pins the audit fix: empty
// dangling result must marshal as `[]`, not `null`, so consumers can
// `jq 'length'` without special-casing nil.
func TestWikiDanglingReturnsEmptyArrayNotNull(t *testing.T) {
	root := initStore(t)
	r := mustRun(t, root, "--format", "json", "wiki", "dangling")
	got := strings.TrimSpace(r.stdout)
	if got != "[]" {
		t.Errorf("wiki dangling on empty store = %q, want %q", got, "[]")
	}
}

// TestWikiOrphansReturnsEmptyArrayNotNull mirrors the dangling test —
// orphans on an empty store must marshal as `[]`, not `null`. Empty
// store is the simplest no-orphans condition; any seed creates an
// orphan since the seeded entity has no inbound links.
func TestWikiOrphansReturnsEmptyArrayNotNull(t *testing.T) {
	root := initStore(t)
	r := mustRun(t, root, "--format", "json", "wiki", "orphans")
	got := strings.TrimSpace(r.stdout)
	if got != "[]" {
		t.Errorf("wiki orphans on empty store = %q, want %q", got, "[]")
	}
}

// TestNoAutoInjectedFrontmatterFields pins the diff-bomb prevention
// fix: a freshly-created entity file must NOT contain id, uuid,
// created_at, or updated_at in its frontmatter. Filename is the
// canonical id; nothing else gets stamped in by mdql.
func TestNoAutoInjectedFrontmatterFields(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add",
		"--name", "Jane Smith",
		"--email", "jane@example.com")

	raw, err := os.ReadFile(filepath.Join(root, "people", "jane-smith", "index.md"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	for _, banned := range []string{"id:", "uuid:", "created_at:", "updated_at:"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("frontmatter still contains %q (should be filename-derived):\n%s", banned, raw)
		}
	}
}

// TestSubFileCRUDRoundtrip walks every verb of a sub-file kind via the
// CLI: add → list → show → update → delete. Mirrors TestPersonRoundtrip
// shape so the same kinds of drift (unregistered verb, missing command,
// wrong arg count) surface the same way.
func TestSubFileCRUDRoundtrip(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site")

	add := mustRun(t, root, "--format", "json", "project", "meeting", "add", "launch-site",
		"--subject", "kickoff", "--date", "2026-04-17")
	added := decodeJSON(t, add)
	if added[0]["id"] != "2026-04-17-kickoff" {
		t.Errorf("sub-file slug = %v, want 2026-04-17-kickoff", added[0]["id"])
	}

	mustRun(t, root, "project", "meeting", "add", "launch-site",
		"--subject", "review", "--date", "2026-04-20")

	list := mustRun(t, root, "--format", "json", "project", "meeting", "list", "launch-site")
	items := decodeJSON(t, list)
	if len(items) != 2 {
		t.Fatalf("meeting list: got %d, want 2\n%s", len(items), list.stdout)
	}

	show := mustRun(t, root, "--format", "json", "project", "meeting", "show", "launch-site", "2026-04-17-kickoff")
	shown := decodeJSONObject(t, show)
	if shown["subject"] != "kickoff" {
		t.Errorf("subject = %v, want kickoff", shown["subject"])
	}

	mustRun(t, root, "project", "meeting", "update", "launch-site", "2026-04-17-kickoff",
		"--subject", "kickoff-v2")
	show = mustRun(t, root, "--format", "json", "project", "meeting", "show", "launch-site", "2026-04-17-kickoff")
	shown = decodeJSONObject(t, show)
	if shown["subject"] != "kickoff-v2" {
		t.Errorf("subject after update = %v, want kickoff-v2", shown["subject"])
	}

	mustRun(t, root, "project", "meeting", "delete", "launch-site", "2026-04-17-kickoff")
	r := run(t, root, "project", "meeting", "show", "launch-site", "2026-04-17-kickoff")
	if r.exitCode != 3 {
		t.Errorf("post-delete show: exit %d, want 3 (ErrNotFound)", r.exitCode)
	}
}

// TestSubFileMissingRequiredFails pins that a missing required field
// rejects the command. Cobra's own MarkFlagRequired fires before RunE,
// so this surfaces as a cobra-style "required flag not set" error at
// exit 1 — same failure mode that exists for entity commands with
// required+no-default fields.
func TestSubFileMissingRequiredFails(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site")
	r := run(t, root, "project", "meeting", "add", "launch-site", "--date", "2026-04-17")
	if r.exitCode == 0 {
		t.Errorf("expected failure, got exit 0\nstdout: %s\nstderr: %s", r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, "subject") {
		t.Errorf("stderr should mention the missing `subject` field, got: %s", r.stderr)
	}
}

// TestSchemaDescribePopulatesSubFiles pins that `schema describe` on a
// schema declaring `files:` emits a populated `sub_files` map with the
// per-sub-file field schema and the generated command list. This is
// the commit-3 contract: agents use this payload to learn the full
// sub-file command surface without reading schema.yml directly.
func TestSchemaDescribePopulatesSubFiles(t *testing.T) {
	root := initStore(t)
	r := mustRun(t, root, "schema", "describe")

	var desc map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &desc); err != nil {
		t.Fatalf("parse describe: %v\nstdout: %s", err, r.stdout)
	}
	project := desc["entities"].(map[string]any)["project"].(map[string]any)
	subs, ok := project["sub_files"].(map[string]any)
	if !ok {
		t.Fatalf("project.sub_files missing: %v", project)
	}
	for _, want := range []string{"meeting", "decision", "note"} {
		if _, ok := subs[want]; !ok {
			t.Errorf("sub_files missing kind %q: %v", want, subs)
		}
	}
	meeting := subs["meeting"].(map[string]any)
	if meeting["dir"] != "meetings" {
		t.Errorf("meeting.dir = %v, want meetings", meeting["dir"])
	}
	fields := meeting["frontmatter_fields"].(map[string]any)
	if fields["subject"] == nil {
		t.Errorf("meeting.frontmatter_fields.subject missing: %v", fields)
	}
	cmds := meeting["commands"].([]any)
	if len(cmds) == 0 {
		t.Errorf("meeting.commands empty")
	}
	note := subs["note"].(map[string]any)
	if note["catchall"] != true {
		t.Errorf("note.catchall = %v, want true", note["catchall"])
	}
}

// TestProjectShowReturnsSubFileGraph pins Wave D of commit 3: `<kind>
// show --format json` on a sprawl entity returns a `sub_files` array
// containing every declared sub-file of the parent, with kind/slug/
// root-relative path/fields. Body is intentionally absent — the graph
// is metadata; drill down via `project meeting show` for body.
func TestProjectShowReturnsSubFileGraph(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site")
	mustRun(t, root, "project", "meeting", "add", "launch-site", "--subject", "kickoff", "--date", "2026-04-17")
	mustRun(t, root, "project", "meeting", "add", "launch-site", "--subject", "review", "--date", "2026-04-20")
	mustRun(t, root, "project", "decision", "add", "launch-site", "--title", "use-tailwind")

	r := mustRun(t, root, "--format", "json", "project", "show", "launch-site")
	obj := decodeJSONObject(t, r)
	subsAny, ok := obj["sub_files"].([]any)
	if !ok {
		t.Fatalf("sub_files missing or wrong type: %T %v", obj["sub_files"], obj["sub_files"])
	}
	if len(subsAny) != 3 {
		t.Fatalf("sub_files: got %d, want 3\n%v", len(subsAny), subsAny)
	}
	// ForEachSubFile sorts by (kind, slug): decision < meeting alphabetically.
	wantKinds := []string{"decision", "meeting", "meeting"}
	for i, item := range subsAny {
		m := item.(map[string]any)
		if m["kind"] != wantKinds[i] {
			t.Errorf("sub_files[%d].kind = %v, want %v", i, m["kind"], wantKinds[i])
		}
		if m["slug"] == "" || m["slug"] == nil {
			t.Errorf("sub_files[%d].slug missing", i)
		}
		path, _ := m["path"].(string)
		if path == "" || filepath.IsAbs(path) {
			t.Errorf("sub_files[%d].path = %q, want non-empty and root-relative", i, path)
		}
		if _, ok := m["fields"].(map[string]any); !ok {
			t.Errorf("sub_files[%d].fields missing: %v", i, m)
		}
		if _, has := m["body"]; has {
			t.Errorf("sub_files[%d] should not include body: %v", i, m)
		}
	}
}

// TestEntityWithoutSubFilesHasNoSubFilesKey pins the other side of
// Wave D: an entity that declares no `files:` in its schema must NOT
// have a `sub_files` key on its `show --format json` output. Absence
// is the agent's signal that the entity has no child documents.
func TestEntityWithoutSubFilesHasNoSubFilesKey(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add",
		"--name", "Jane Smith",
		"--email", "jane@example.com")
	r := mustRun(t, root, "--format", "json", "person", "show", "jane-smith")
	obj := decodeJSONObject(t, r)
	if _, has := obj["sub_files"]; has {
		t.Errorf("flat person has sub_files key: %v", obj)
	}
}

// TestSubFileCommandsInEntityHelp pins that declaring `files:` on an
// entity exposes the sub-file kinds as subcommands under that entity —
// so `mdql project --help` lists `meeting`, `decision`, `note`.
func TestSubFileCommandsInEntityHelp(t *testing.T) {
	root := initStore(t)
	help := mustRun(t, root, "project", "--help").stdout
	for _, want := range []string{"meeting", "decision", "note"} {
		if !strings.Contains(help, want) {
			t.Errorf("project --help missing sub-file kind %q:\n%s", want, help)
		}
	}
}

// TestLintCleanStoreExitZero asserts that a well-formed store returns
// exit 0 with a zero-count summary. Pins the baseline so future checks
// don't accidentally false-positive on a clean fixture.
func TestLintCleanStoreExitZero(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add",
		"--name", "Jane Smith",
		"--email", "jane@example.com")
	r := mustRun(t, root, "--quiet", "lint")
	want := "errors=0 warns=0 info=0"
	if !strings.Contains(r.stdout, want) {
		t.Errorf("lint summary = %q, want contains %q", r.stdout, want)
	}
}

// TestLintCatchallAbsorbedInfo pins the INFO classification for a loose
// .md file sitting under a sprawl entity folder. The catchall kind
// attribution is the primary way agents discover drift-via-loose-files,
// so its classification is load-bearing.
func TestLintCatchallAbsorbedInfo(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site", "--stage", "active")
	loose := filepath.Join(root, "projects", "launch-site", "loose.md")
	body := "---\ntitle: Loose\n---\n\nhand-dropped file"
	if err := os.WriteFile(loose, []byte(body), 0o644); err != nil {
		t.Fatalf("write loose: %v", err)
	}
	r := mustRun(t, root, "--format", "json", "lint")
	var rep struct {
		Findings []map[string]any `json:"findings"`
		Summary  map[string]int   `json:"summary"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatalf("parse lint json: %v\n%s", err, r.stdout)
	}
	if rep.Summary["info"] != 1 || rep.Summary["warn"] != 0 || rep.Summary["error"] != 0 {
		t.Fatalf("summary = %v, want info=1 warn=0 error=0", rep.Summary)
	}
	if rep.Findings[0]["code"] != "catchall_absorbed" {
		t.Errorf("code = %v, want catchall_absorbed", rep.Findings[0]["code"])
	}
}

// TestLintOrphanSubFilesExitTwo drives the worst-severity path: a folder
// without index.md holding sub-file data. ERROR must exit 2 so CI gates
// can reject this specific class of drift even when warnings would have
// shipped.
func TestLintOrphanSubFilesExitTwo(t *testing.T) {
	root := initStore(t)
	orphan := filepath.Join(root, "projects", "no-index", "meetings")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\nsubject: Whoops\ndate: 2026-05-01\n---\n"
	if err := os.WriteFile(filepath.Join(orphan, "2026-05-01-whoops.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write sub-file: %v", err)
	}
	r := run(t, root, "--quiet", "lint")
	if r.exitCode != 2 {
		t.Fatalf("exit = %d, want 2\nstdout: %s\nstderr: %s", r.exitCode, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "errors=1") {
		t.Errorf("summary missing errors=1: %q", r.stdout)
	}
}

// TestLintWarningExitOne covers the middle exit code. A missing required
// field is the most likely real-world WARN (hand-edited frontmatter),
// so we pin it rather than dangling_link.
func TestLintWarningExitOne(t *testing.T) {
	root := initStore(t)
	folder := filepath.Join(root, "people", "jane")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// name is required, but the record below omits it.
	body := "---\nemail: jane@example.com\n---\n"
	if err := os.WriteFile(filepath.Join(folder, "index.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := run(t, root, "--quiet", "lint")
	if r.exitCode != 1 {
		t.Fatalf("exit = %d, want 1\nstdout: %s\nstderr: %s", r.exitCode, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "errors=0") {
		t.Errorf("expected errors=0 in summary: %q", r.stdout)
	}
}

// TestWikiLinkSubFilePathResolves pins the grammar extension: a body
// reference of the form [[parent-slug/subdir/sub-slug]] must resolve
// against a real sub-file path and NOT surface as dangling. Before
// commit 4 this false-positived.
func TestWikiLinkSubFilePathResolves(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site")
	mustRun(t, root, "project", "decision", "add", "launch-site", "--title", "use-tailwind")
	mustRun(t, root, "project", "meeting", "add", "launch-site",
		"--subject", "review", "--date", "2026-04-20",
		"--body", "Follows up on [[launch-site/decisions/use-tailwind]].",
	)
	r := mustRun(t, root, "--format", "json", "wiki", "dangling").stdout
	if got := strings.TrimSpace(r); got != "[]" {
		t.Errorf("sub-file path reference should resolve, got dangling: %s", got)
	}
}

// TestWikiLinkKindQualifiedResolves pins the 2-segment grammar: a
// `[[kind/slug]]` reference resolves against a known entity and does
// not surface as dangling.
func TestWikiLinkKindQualifiedResolves(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "jane@x")
	mustRun(t, root, "project", "add", "--name", "Relate",
		"--body", "Owner: [[person/jane-smith]].",
	)
	r := mustRun(t, root, "--format", "json", "wiki", "dangling").stdout
	if got := strings.TrimSpace(r); got != "[]" {
		t.Errorf("kind-qualified reference should resolve, got dangling: %s", got)
	}
}

// TestWikiBacklinksSurfaceSubFileRefs pins that a sub-file body linking
// to an entity surfaces via `backlinks <parent-slug>`. Demonstrates the
// ExpandLinkKeys fan-out: the link `[[jane-smith]]` inside a meeting
// produces a `jane-smith` key in the meeting doc's LinksTo.
func TestWikiBacklinksSurfaceSubFileRefs(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "jane@x")
	mustRun(t, root, "project", "add", "--name", "Launch Site")
	mustRun(t, root, "project", "meeting", "add", "launch-site",
		"--subject", "kickoff", "--date", "2026-04-17",
		"--body", "Led by [[jane-smith]].",
	)
	r := mustRun(t, root, "--format", "json", "wiki", "backlinks", "jane-smith").stdout
	if !strings.Contains(r, `"sub_kind":"meeting"`) {
		t.Errorf("expected meeting sub-file in backlinks of jane-smith, got: %s", r)
	}
}

// TestCrossSubFileLinkRoundtrip pins the `project.decision` target form:
// a decision's `supersedes` link field pointing at another decision must
// index + surface via wiki backlinks for the superseded decision.
func TestCrossSubFileLinkRoundtrip(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site")
	mustRun(t, root, "project", "decision", "add", "launch-site", "--title", "use-bootstrap")
	mustRun(t, root, "project", "decision", "add", "launch-site",
		"--title", "use-tailwind",
		"--supersedes", "[[launch-site/decisions/use-bootstrap]]",
	)

	// backlinks on the superseded decision should include the superseding one.
	r := mustRun(t, root, "--format", "json", "wiki", "backlinks",
		"launch-site/decisions/use-bootstrap").stdout
	if !strings.Contains(r, `"slug":"use-tailwind"`) {
		t.Errorf("expected use-tailwind to reference use-bootstrap, got: %s", r)
	}
}

// TestSearchSubFilter pins the --sub flag: returns only sub-file hits
// of the specified sub-kind. Seeds an entity and a meeting that share
// a body token ("shipping") to force the filter to matter.
func TestSearchSubFilter(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site",
		"--body", "Focused on shipping next quarter.")
	mustRun(t, root, "project", "meeting", "add", "launch-site",
		"--subject", "kickoff", "--date", "2026-04-17",
		"--body", "Agenda: shipping cadence.")

	r := mustRun(t, root, "--format", "json", "search", "shipping", "--sub", "meeting").stdout
	var hits []map[string]any
	if err := json.Unmarshal([]byte(r), &hits); err != nil {
		t.Fatalf("parse: %v\n%s", err, r)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %s", len(hits), r)
	}
	if hits[0]["sub_kind"] != "meeting" {
		t.Errorf("expected sub_kind=meeting, got: %v", hits[0])
	}
}

// TestSearchTypeDisjunction pins the --type convenience: matches either
// Type (entity kind) or SubKind (sub-file kind). No cross-kind --type
// argument was needed before sub-files existed; now it's the most
// common filter an agent reaches for.
func TestSearchTypeDisjunction(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site")
	mustRun(t, root, "project", "meeting", "add", "launch-site",
		"--subject", "kickoff", "--date", "2026-04-17",
		"--body", "onboarding agenda")

	r := mustRun(t, root, "--format", "json", "search", "onboarding", "--type", "meeting").stdout
	var hits []map[string]any
	if err := json.Unmarshal([]byte(r), &hits); err != nil {
		t.Fatalf("parse: %v\n%s", err, r)
	}
	if len(hits) == 0 {
		t.Errorf("--type meeting should match sub-files by sub_kind, got: %s", r)
	}
}

// TestDryRunCreateNoWrites pins the core --dry-run promise: no file is
// written and no index entry is created. The plan is emitted on stdout
// with the full {action, path, frontmatter, body} shape so agents can
// plan-then-execute.
func TestDryRunCreateNoWrites(t *testing.T) {
	root := initStore(t)
	r := mustRun(t, root, "--format", "json", "person", "add",
		"--name", "Jane Smith", "--email", "jane@x", "--dry-run")

	var plan map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &plan); err != nil {
		t.Fatalf("parse plan: %v\n%s", err, r.stdout)
	}
	if plan["action"] != "create" {
		t.Errorf("action = %v, want create", plan["action"])
	}
	if plan["slug"] != "jane-smith" {
		t.Errorf("slug = %v, want jane-smith", plan["slug"])
	}
	if got := fmt.Sprint(plan["path"]); got != filepath.Join("people", "jane-smith", "index.md") {
		t.Errorf("path = %s", got)
	}

	// disk untouched
	if _, err := os.Stat(filepath.Join(root, "people", "jane-smith")); !os.IsNotExist(err) {
		t.Errorf("folder should not exist after --dry-run, stat err = %v", err)
	}

	// subsequent real create must succeed (nothing reserved)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "jane@x")
}

// TestDryRunUpdateIncludesBefore asserts the update plan carries a
// before snapshot alongside the would-be frontmatter.
func TestDryRunUpdateIncludesBefore(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "jane@x")

	r := mustRun(t, root, "--format", "json", "person", "update", "jane-smith",
		"--email", "jane@y", "--dry-run")
	var plan map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &plan); err != nil {
		t.Fatalf("parse plan: %v\n%s", err, r.stdout)
	}
	before, ok := plan["before"].(map[string]any)
	if !ok {
		t.Fatalf("missing before block: %s", r.stdout)
	}
	beforeFront := before["frontmatter"].(map[string]any)
	if beforeFront["email"] != "jane@x" {
		t.Errorf("before.email = %v, want jane@x", beforeFront["email"])
	}
	after := plan["frontmatter"].(map[string]any)
	if after["email"] != "jane@y" {
		t.Errorf("frontmatter.email = %v, want jane@y", after["email"])
	}

	// disk still reflects the original
	got := mustRun(t, root, "--format", "json", "person", "show", "jane-smith").stdout
	var rec map[string]any
	_ = json.Unmarshal([]byte(got), &rec)
	if rec["email"] != "jane@x" {
		t.Errorf("live email = %v, want jane@x (unchanged)", rec["email"])
	}
}

// TestDryRunArchiveKeepsFiles verifies that archive --dry-run doesn't
// move the entity folder or reindex.
func TestDryRunArchiveKeepsFiles(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "jane@x")

	r := mustRun(t, root, "--format", "json", "person", "archive", "jane-smith", "--dry-run")
	var plan map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &plan); err != nil {
		t.Fatalf("parse plan: %v\n%s", err, r.stdout)
	}
	if plan["action"] != "archive" {
		t.Errorf("action = %v, want archive", plan["action"])
	}
	if plan["to_path"] == nil {
		t.Errorf("archive plan missing to_path: %s", r.stdout)
	}

	if _, err := os.Stat(filepath.Join(root, "people", "jane-smith", "index.md")); err != nil {
		t.Errorf("live folder gone after --dry-run: %v", err)
	}
}

// TestDryRunSubFileLifecycle exercises sub-file add/update/delete dry-
// run outputs. The create path crosses the commit-4 indexSubFile
// wrapper, which dry-run must bypass.
func TestDryRunSubFileLifecycle(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site")

	r := mustRun(t, root, "--format", "json", "project", "meeting", "add", "launch-site",
		"--subject", "kickoff", "--date", "2026-04-17", "--dry-run")
	var plan map[string]any
	_ = json.Unmarshal([]byte(r.stdout), &plan)
	if plan["action"] != "create" || plan["kind"] != "meeting" {
		t.Errorf("sub-file create plan wrong: %v", plan)
	}
	if plan["parent_slug"] != "launch-site" {
		t.Errorf("parent_slug = %v, want launch-site", plan["parent_slug"])
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "launch-site", "meetings")); !os.IsNotExist(err) {
		t.Errorf("meeting folder exists after --dry-run: %v", err)
	}

	// Actually create one to exercise update/delete plan.
	mustRun(t, root, "project", "meeting", "add", "launch-site",
		"--subject", "kickoff", "--date", "2026-04-17")
	meetingPath := filepath.Join(root, "projects", "launch-site", "meetings", "2026-04-17-kickoff.md")
	info, err := os.Stat(meetingPath)
	if err != nil {
		t.Fatalf("meeting not created: %v", err)
	}
	origMtime := info.ModTime()

	r = mustRun(t, root, "--format", "json", "project", "meeting", "delete", "launch-site",
		"2026-04-17-kickoff", "--dry-run")
	_ = json.Unmarshal([]byte(r.stdout), &plan)
	if plan["action"] != "delete" {
		t.Errorf("delete plan wrong: %v", plan)
	}
	info2, err := os.Stat(meetingPath)
	if err != nil {
		t.Fatalf("meeting removed by --dry-run delete: %v", err)
	}
	if !info2.ModTime().Equal(origMtime) {
		t.Errorf("meeting mtime changed under --dry-run delete")
	}
}

// TestFieldsProjectionJSON asserts that --fields projects to the
// requested subset and preserves their order as a set (keys only).
func TestFieldsProjectionJSON(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--name", "Jane Smith", "--email", "jane@x", "--role", "lead")
	mustRun(t, root, "person", "add", "--name", "Bob Quinn", "--email", "bob@x")

	r := mustRun(t, root, "--format", "json", "person", "list", "--fields", "id,role")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &rows); err != nil {
		t.Fatalf("parse: %v\n%s", err, r.stdout)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if _, has := row["name"]; has {
			t.Errorf("projected row should not carry name: %v", row)
		}
		if _, has := row["id"]; !has {
			t.Errorf("projected row missing id: %v", row)
		}
		if _, has := row["role"]; !has {
			t.Errorf("projected row missing role: %v", row)
		}
	}
}

// TestFieldsRejectsUnknown pins the validation-before-store-open
// promise: an unknown --fields value exits non-zero without spinning
// up bleve.
func TestFieldsRejectsUnknown(t *testing.T) {
	root := initStore(t)
	r := run(t, root, "person", "list", "--fields", "id,bogus")
	if r.exitCode == 0 {
		t.Fatalf("expected non-zero exit, got 0\nstdout: %s\nstderr: %s", r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, "bogus") && !strings.Contains(r.stdout, "bogus") {
		t.Errorf("error should mention the unknown field: stderr=%s stdout=%s", r.stderr, r.stdout)
	}
}

// TestFieldsProjectionSubFile covers --fields on a sub-file list so the
// projection wiring stays consistent across the two command trees.
func TestFieldsProjectionSubFile(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "project", "add", "--name", "Launch Site")
	mustRun(t, root, "project", "meeting", "add", "launch-site",
		"--subject", "kickoff", "--date", "2026-04-17")

	r := mustRun(t, root, "--format", "json", "project", "meeting", "list", "launch-site",
		"--fields", "subject")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &rows); err != nil {
		t.Fatalf("parse: %v\n%s", err, r.stdout)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d, want 1: %s", len(rows), r.stdout)
	}
	if rows[0]["subject"] != "kickoff" {
		t.Errorf("subject = %v", rows[0]["subject"])
	}
	if _, has := rows[0]["date"]; has {
		t.Errorf("date should be projected out: %v", rows[0])
	}
}

// assertGoldenEqual compares two record lists field-by-field for the
// keys present in want. Extra keys in got (e.g. optional fields) are
// ignored so the golden doesn't have to enumerate every schema field.
func assertGoldenEqual(t *testing.T, want, got []map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d\ngot: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		for _, k := range sortedKeys(want[i]) {
			wv := want[i][k]
			gv := got[i][k]
			if fmt.Sprint(wv) != fmt.Sprint(gv) {
				t.Errorf("row %d key %q: got %v, want %v", i, k, gv, wv)
			}
		}
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
