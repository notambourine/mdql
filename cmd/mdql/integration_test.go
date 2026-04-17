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

	"gopkg.in/yaml.v3"
)

// fixtureSchema is the CRM-shaped schema the integration tests run
// against. Mirrors schema/schema_test.go#crmFixture minus the bits
// unrelated to CLI parity (relationships, interactions) so the tests
// exercise the same commands a CRM user would.
const fixtureSchema = `
version: 1
store:
  runtime_dir: ".mdql"
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
  organization:
    dir: organizations
    title: "{{.name}}"
    slug:  "{{.name}}"
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
    slug:  "{{.title}}"
    fields:
      title:     {type: string, required: true}
      priority:  {type: enum, values: [low, medium, high], default: medium}
      completed: {type: bool, default: false}
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

// scrub removes per-run variable fields so a record can be compared to a
// golden shape. uuid/created_at/updated_at change every invocation; body
// is absent from list output but can leak in when a create returns.
func scrub(recs []map[string]any) []map[string]any {
	for _, r := range recs {
		delete(r, "uuid")
		delete(r, "created_at")
		delete(r, "updated_at")
		delete(r, "body")
	}
	return recs
}

func TestPersonRoundtrip(t *testing.T) {
	root := initStore(t)

	add := mustRun(t, root, "--format", "json", "person", "add",
		"--first-name", "Jane", "--last-name", "Smith",
		"--email", "jane@example.com")
	added := decodeJSON(t, add)
	if len(added) != 1 {
		t.Fatalf("expected 1 record, got %d", len(added))
	}
	if added[0]["id"] != "jane-smith" {
		t.Errorf("slug = %v, want jane-smith", added[0]["id"])
	}
	if uid, ok := added[0]["uuid"].(string); !ok || uid == "" {
		t.Errorf("uuid missing: %v", added[0]["uuid"])
	}

	path := filepath.Join(root, "people", "jane-smith.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}

	mustRun(t, root, "person", "update", "jane-smith", "--email", "jane@new.example.com")

	show := mustRun(t, root, "--format", "json", "person", "show", "jane-smith")
	shown := decodeJSON(t, show)
	if shown[0]["email"] != "jane@new.example.com" {
		t.Errorf("email not updated: %v", shown[0]["email"])
	}

	mustRun(t, root, "person", "archive", "jane-smith")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after archive: %v", err)
	}
	archived := filepath.Join(root, "_archive", "people", "jane-smith.md")
	if _, err := os.Stat(archived); err != nil {
		t.Errorf("archived file missing: %v", err)
	}
}

func TestOrgRoundtrip(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "organization", "add", "--name", "Acme Corp", "--domain", "acme.test")
	show := mustRun(t, root, "--format", "json", "organization", "show", "acme-corp")
	shown := decodeJSON(t, show)
	if shown[0]["domain"] != "acme.test" {
		t.Errorf("domain = %v", shown[0]["domain"])
	}
	mustRun(t, root, "organization", "archive", "acme-corp")
}

func TestDealRoundtrip(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "deal", "add", "--title", "Big Renewal", "--stage", "lead", "--value", "50000")
	list := mustRun(t, root, "--format", "json", "deal", "list")
	deals := decodeJSON(t, list)
	if len(deals) != 1 {
		t.Fatalf("expected 1 deal, got %d", len(deals))
	}
	id := deals[0]["id"].(string)
	mustRun(t, root, "deal", "update", id, "--stage", "won")
	mustRun(t, root, "deal", "archive", id)
}

func TestTaskRoundtrip(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "task", "add", "--title", "Follow up with Jane", "--priority", "high")
	list := mustRun(t, root, "--format", "json", "task", "list")
	tasks := decodeJSON(t, list)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	id := tasks[0]["id"].(string)
	mustRun(t, root, "task", "update", id, "--priority", "medium")
	mustRun(t, root, "task", "archive", id)
}

// TestListGoldenShape asserts `person list --format json` matches the
// expected shape after two inserts, using a scrubbed comparison so
// uuid/timestamps (run-variable) don't leak into the assertion.
func TestListGoldenShape(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--first-name", "Ana", "--last-name", "Zed", "--email", "ana@x")
	mustRun(t, root, "person", "add", "--first-name", "Bob", "--last-name", "Roe", "--email", "bob@x")

	list := mustRun(t, root, "--format", "json", "person", "list")
	got := scrub(decodeJSON(t, list))

	want := []map[string]any{
		{"id": "ana-zed", "first_name": "Ana", "last_name": "Zed", "email": "ana@x"},
		{"id": "bob-roe", "first_name": "Bob", "last_name": "Roe", "email": "bob@x"},
	}
	assertGoldenEqual(t, want, got)
}

// TestSlugCollisionPreservesUUID is the first of the three merge-
// collision semantic-drift cases: distinct records with identical slug
// inputs must get suffixed slugs AND distinct UUIDs so downstream
// identity (any system keying by uuid) stays intact.
func TestSlugCollisionPreservesUUID(t *testing.T) {
	root := initStore(t)
	a := decodeJSON(t, mustRun(t, root, "--format", "json", "person", "add",
		"--first-name", "Jane", "--last-name", "Smith", "--email", "jane1@x"))
	b := decodeJSON(t, mustRun(t, root, "--format", "json", "person", "add",
		"--first-name", "Jane", "--last-name", "Smith", "--email", "jane2@x"))

	if a[0]["id"] != "jane-smith" {
		t.Errorf("first slug = %v, want jane-smith", a[0]["id"])
	}
	if b[0]["id"] != "jane-smith-2" {
		t.Errorf("collision slug = %v, want jane-smith-2", b[0]["id"])
	}
	if a[0]["uuid"] == b[0]["uuid"] {
		t.Errorf("uuids collided: %v", a[0]["uuid"])
	}
}

// TestWikiLinksAliasToFirstSlug exercises the semantic-drift limitation
// pinned in CLAUDE.md: once two "Jane Smith" records exist, a body
// `[[jane-smith]]` reference only resolves to record A. wiki.Check
// sees the target slug exists so reports no dangling — the drift is
// invisible by design.
func TestWikiLinksAliasToFirstSlug(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--first-name", "Jane", "--last-name", "Smith", "--email", "a@x")
	mustRun(t, root, "person", "add", "--first-name", "Jane", "--last-name", "Smith", "--email", "b@x")

	mustRun(t, root, "deal", "add",
		"--title", "Relate",
		"--stage", "lead",
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
	mustRun(t, root, "person", "add", "--first-name", "Jane", "--last-name", "Smith", "--email", "a@x")
	mustRun(t, root, "deal", "add",
		"--title", "Relate",
		"--stage", "lead",
		"--body", "Ref [[jane-smith]].",
	)

	// Rename the file and rewrite its frontmatter id. mdql has no
	// rename command — we emulate the out-of-band edit that any real
	// user would do (e.g. git mv + hand-edit), then confirm the wiki
	// check surfaces the now-broken reference.
	oldPath := filepath.Join(root, "people", "jane-smith.md")
	newPath := filepath.Join(root, "people", "jane-smith-renamed.md")
	rewriteID(t, oldPath, newPath, "jane-smith-renamed")

	dangling := mustRun(t, root, "--format", "json", "wiki", "dangling").stdout
	if !strings.Contains(dangling, `"target_slug":"jane-smith"`) {
		t.Errorf("expected dangling ref to jane-smith, got: %s", dangling)
	}
}

func TestFormatParity(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "person", "add", "--first-name", "Jane", "--last-name", "Smith", "--email", "jane@example.com")
	mustRun(t, root, "person", "add", "--first-name", "Bob", "--last-name", "Jones", "--email", "bob@example.com")

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
	mustRun(t, root, "person", "add", "--first-name", "Jane", "--last-name", "Smith", "--email", "j@x")
	mustRun(t, root, "person", "add", "--first-name", "Bob", "--last-name", "Jones", "--email", "b@x")

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
	mustRun(t, root, "person", "add", "--first-name", "Lonely", "--last-name", "Soul", "--email", "l@x")
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
// bleve links_to field — so Backlinks("acme-corp") missed the bracket-
// wrapped entry. The contradicted doc claim is wiki/wiki.go:30
// ("frontmatter or body").
func TestFrontmatterLinkBacklinks(t *testing.T) {
	root := initStore(t)
	mustRun(t, root, "organization", "add", "--name", "Acme Corp", "--domain", "acme.test")
	// Pass the wiki-wrapped form at the CLI — simulates a user who
	// writes the frontmatter-native syntax and also matches the shape
	// seeded by example/run.sh.
	mustRun(t, root, "deal", "add",
		"--title", "Acme Renewal",
		"--stage", "lead",
		"--org", "[[acme-corp]]",
	)
	mustRun(t, root, "index", "rebuild")
	backlinks := mustRun(t, root, "--format", "json", "wiki", "backlinks", "acme-corp").stdout
	if !strings.Contains(backlinks, `"slug":"acme-renewal"`) {
		t.Errorf("expected acme-renewal in backlinks, got: %s", backlinks)
	}
}

// TestRequiredFieldWithDefaultSkipsFlagValidation pins the fix for
// the required+default footgun: deal.stage is {required: true,
// default: lead}, so `deal add --title X` (no --stage) must succeed
// and the schema default must land in the record. Before the fix,
// cobra rejected the command at flag-parse time with "required
// flag(s) stage not set" — defaults are applied later inside
// store.Create, so requiring the flag made the default unreachable.
func TestRequiredFieldWithDefaultSkipsFlagValidation(t *testing.T) {
	root := initStore(t)
	r := run(t, root, "--format", "json", "deal", "add", "--title", "Default Stage Deal")
	if r.exitCode != 0 {
		t.Fatalf("deal add without --stage: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	added := decodeJSON(t, r)
	if added[0]["stage"] != "lead" {
		t.Errorf("stage = %v, want lead (schema default)", added[0]["stage"])
	}
}

func TestValidationExit2(t *testing.T) {
	root := initStore(t)
	r := run(t, root, "deal", "add", "--title", "Bad Deal", "--stage", "not-a-stage")
	if r.exitCode != 2 {
		t.Errorf("exit code = %d, want 2 (ErrValidation)\nstderr: %s", r.exitCode, r.stderr)
	}
}

// TestDroppedCommandsAbsent pins the intentional parity gap with crm:
// status, context, log, and deal-pipeline were removed during the
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
	dealHelp := mustRun(t, root, "deal", "--help").stdout
	if strings.Contains(dealHelp, "\n  pipeline ") {
		t.Errorf("dropped deal subcommand 'pipeline' present in deal --help:\n%s", dealHelp)
	}
}

// rewriteID renames oldPath to newPath and rewrites the id frontmatter
// key to newID. Used by TestRenameSurfacesDanglingLinks to simulate an
// out-of-band file rename that mdql itself does not offer as a command.
func rewriteID(t *testing.T, oldPath, newPath, newID string) {
	t.Helper()
	raw, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("read %s: %v", oldPath, err)
	}
	front, body, ok := bytes.Cut(raw[len("---\n"):], []byte("---\n"))
	if !ok {
		t.Fatalf("unterminated frontmatter in %s", oldPath)
	}
	var fm map[string]any
	if err := yaml.Unmarshal(front, &fm); err != nil {
		t.Fatalf("parse frontmatter: %v", err)
	}
	fm["id"] = newID
	newFront, err := yaml.Marshal(fm)
	if err != nil {
		t.Fatalf("encode frontmatter: %v", err)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(newFront)
	buf.WriteString("---\n")
	buf.Write(body)
	if err := os.WriteFile(newPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", newPath, err)
	}
	if err := os.Remove(oldPath); err != nil {
		t.Fatalf("remove %s: %v", oldPath, err)
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
