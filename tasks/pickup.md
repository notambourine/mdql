# mdql pickup

Context for resuming the mdql extraction in a fresh Claude session. Read this end-to-end, then start at **Next step** below.

## What mdql is

Schema-driven typed CRUD + full-text search + wiki graph over markdown files with YAML frontmatter. One binary, one `schema.yml`, a directory of notes — no app, no daemon, no cloud. Extracted from [`notambourine/crm-cli`](https://github.com/notambourine/crm-cli) (itself a fork of [`jdanielnd/crm-cli`](https://github.com/jdanielnd/crm-cli), MIT) as a clean hat-tip (fresh git history, not a fork).

The original CRM was ~3.7k LOC of Go wrapping five entities (person/org/deal/task/interaction) around ~810 LOC of genuinely generic markdown-graph engine. Every new entity cost 450–670 LOC of near-identical CLI + store + model + tag-dispatch scaffolding. mdql replaces that scaffolding with a YAML schema + a dynamic cobra generator. crm-cli becomes a schema file + a ~20-line main.go that embeds it.

## Settled decisions (do not relitigate)

1. **Clean hat-tip**, not a fork. No history inheritance from crm-cli. LICENSE + README credit `jdanielnd/crm-cli` as the spark.
2. **Name**: `mdql` (`mdgraph` is taken by GitHub graph-viz projects; `mdbase`, `mdschema` also taken).
3. **Schema format**: YAML (reuses `gopkg.in/yaml.v3`, userland-editable).
4. **Drop CRM custom behaviors**: no pipeline aggregation, no context briefing, no overdue filter, no duplicate-email logic in code. Users pipe through `jq` over `list --format json`. A `queries.md` golden-set seeds AI context with recipes.
5. **Attribution handled**: MIT LICENSE with lineage note; README `## Credit` section.

## Current state (as of the commits on main)

### mdql repo (`github.com/notambourine/mdql`, public)

Commits so far:
1. `chore: initial repo skeleton` — LICENSE, README, .gitignore, go.mod
2. `chore: add cmd/mdql entry point and scope gitignore to root binary` — adds `cmd/mdql/main.go` (placeholder that errors), fixes `.gitignore` so `mdql` binary rule doesn't match the `cmd/mdql/` directory
3. `chore: import generic markdown-graph engine from crm-cli` — ~2000 LOC of the generic slice, imports rewritten from `github.com/notambourine/crm-cli/internal/` to `github.com/notambourine/mdql/`

What's imported and working:
- `store/md/` — `front.go` (atomic frontmatter r/w), `slug.go`, `link.go`, `entity.go` (shared CRUD helpers: writeEntity/readEntity/sortedDedup), `archive.go` (soft-delete), `fs.go` (still has hardcoded CRM `Kind*` constants + `Dirs` map — rewritten in step 6), `store.go` (stub; `Reindex` returns empty — wired in step 6)
- `store/indexer.go` — `Indexer` interface + `NoopIndexer` + `Doc` shape (unchanged, already generic)
- `search/` — `bleve.go`, `query.go` (Search, Backlinks, RelsReverse; unchanged, already generic)
- `wiki/wiki.go` — Backlinks, Orphans, DanglingLinks (still hardcodes `{KindPerson, KindOrg, KindDeal, KindTask}` in Orphans — rewritten in step 6)
- `format/format.go` — table/json/csv/tsv via `go-pretty` (unchanged, already generic)
- `model/` — `link.go` (Link type + YAML marshaling), `errors.go` (sentinel errors + ExitCode)

Test status: **64 passing** across store/md, search, wiki, format, model. `merge_collision_test.go` was dropped because it exercised the typed `s.People()`/`s.Deals()` stores — recreate against the generic store after step 7.

### crm-cli repo (`notambourine/crm-cli`)

Untouched by this work so far. Still on `markdown-db` branch (working tree clean). The `mdql-extraction-staging` branch exists and can be deleted — it was created during the abandoned subtree-split attempt, then left after rollback. The `mdql-split` branch and `../mdql` worktree that existed at one point are already removed.

## Critical file paths (source material for steps 6-7)

Everything below lives in `/Users/tomfuertes/sandbox/git-repos/crm-cli/internal/` — read these as reference when writing the schema-aware replacements in mdql:

- `store/md/fs.go` lines 10–25 — the hardcoded `Kind*` const block + `Dirs` map to replace with schema-derived lookups
- `store/md/store.go` — the old `Reindex` (preserved in crm-cli) shows what the schema-driven version needs to replicate: walk every entity kind, pull typed records, emit `Upsert` per record
- `store/md/tags.go` lines 37, 85, 120 — hardcoded kind-iteration loops in `ListTags`/`DeleteTag`/`CountTagUsage`
- `store/md/tags.go` lines 150–188 — the `switch kind` in `mutateTags` plus five `write*Raw` helpers (221–264) to collapse into one generic `writeRaw(s, kind, slug, fn)` once the generic store exists
- `store/md/person.go` lines 35–46 — the duplicate-email scan pattern to generalize as `unique: true` field support
- `wiki/wiki.go` lines 51 and 90 — `Orphans` and `Check` iterate hardcoded kinds
- `store/md/person.go` (whole file) — reference for what the generic `Create(ctx, kind, input map[string]any)` needs to do: slugify name, EnsureUnique, validate required, duplicate-check unique fields, writeEntity, Upsert to indexer, same for Update/List/Archive

## What's left

### Next step: Step 5 — Schema package

Add to mdql:

**`schema/schema.go`** — defines and parses the schema file.

```go
package schema

type Schema struct {
    Version  int                 `yaml:"version"`
    Store    StoreConfig         `yaml:"store"`
    Entities map[string]Entity   `yaml:"entities"`
}

type StoreConfig struct {
    RuntimeDir string `yaml:"runtime_dir"` // default ".mdql"
    ArchiveDir string `yaml:"archive_dir"` // default "_archive"
}

type Entity struct {
    Dir        string            `yaml:"dir"`
    Title      string            `yaml:"title"`       // text/template
    Slug       string            `yaml:"slug"`        // text/template
    Archivable *bool             `yaml:"archivable"`  // pointer to distinguish unset from false; default true
    AppendOnly bool              `yaml:"append_only"` // default false
    Fields     map[string]Field  `yaml:"fields"`
}

type Field struct {
    Type       string   `yaml:"type"`       // string|int|float|bool|date|enum|link|string[]|link[]|relation[]
    Required   bool     `yaml:"required"`
    Unique     bool     `yaml:"unique"`
    Sorted     bool     `yaml:"sorted"`
    Default    any      `yaml:"default"`
    Values     []string `yaml:"values"`     // for enum
    Target     string   `yaml:"target"`     // for link, link[], relation[]
    EdgeTypes  []string `yaml:"edge_types"` // for relation[]
}

func Parse(data []byte) (*Schema, error) { /* yaml.Unmarshal + Validate */ }

func (s *Schema) Validate() error {
    // Version must be 1.
    // Each entity must have a non-empty Dir.
    // Each field's Type must be in the allowed set.
    // enum requires Values; link/link[]/relation[] require Target;
    //   relation[] also requires EdgeTypes.
    // Link targets must resolve to another entity in the schema.
    // Apply defaults (Archivable=true if unset, RuntimeDir=".mdql", ArchiveDir="_archive").
}
```

**`schema/template.go`** — renders Title and Slug templates against a frontmatter map.

```go
package schema

import (
    "bytes"
    "text/template"
    "time"
)

var funcMap = template.FuncMap{
    "date": func(layout string, v any) string { /* accept time.Time, string RFC3339, string YYYY-MM-DD; return formatted */ },
    // "index" is already a built-in text/template func; no need to override.
}

func Render(tmpl string, data map[string]any) (string, error) { /* template.New().Funcs(funcMap).Parse().Execute */ }
```

**`schema/schema_test.go`** — fixture schema with the five CRM entities from the plan (see `.claude/plans/structured-booping-sedgewick.md` §A for the exact YAML). Assert: valid schema parses, each field type round-trips, missing `target` on a `link` field fails validation, unknown field type fails validation, template rendering works for `{{.first_name}} {{.last_name}}` and `{{.due_at | date "2006-01-02"}}-{{.title}}`.

Commit message: `feat(schema): add YAML schema parser + template renderer`

### Step 6 — Schema-aware generic layer

Once the schema package exists, rewrite the generic-but-coupled code to iterate `schema.Entities` instead of hardcoded kinds. In mdql:

- `store/md/fs.go`: delete the `const Kind*` block and `var Dirs`. Change `Init` to `Init(root string, s *schema.Schema)` that loops `s.Entities`. Change `EntityDir(root, kind)` to also take `*schema.Schema` (or attach it to `*Store`). `RuntimeDir` comes from `s.Store.RuntimeDir`. Update `fs_test.go` to pass a fixture schema.
- `store/md/store.go`: add a `schema *schema.Schema` field to `Store`. Update `Open` signature: `Open(root string, s *schema.Schema, idx store.Indexer)`. Wire `Reindex` to iterate `s.Entities` calling the generic `List` from step 7.
- `wiki/wiki.go`: `Orphans` and `Check` take the schema, iterate `s.Entities` where `!entity.AppendOnly`.

Tags will land in step 7 once the generic entity store exists (tag mutations need generic `writeEntity` by kind).

Commit: `refactor: drive fs/store/wiki from schema instead of hardcoded kinds`

### Step 7 — Generic entity store

Add `store/md/generic.go` implementing schema-driven CRUD on `map[string]any`:

```go
func (s *Store) Create(ctx context.Context, kind string, input map[string]any) (map[string]any, error) {
    // 1. Lookup entity := s.schema.Entities[kind]; error if missing.
    // 2. Validate required fields, enum memberships, unique-field scans.
    // 3. Render entity.Slug template against input → slug. Slugify, EnsureUnique.
    // 4. Set id=slug, uuid=generated, created_at=now, updated_at=now.
    // 5. writeEntity(path, input, body).
    // 6. Render entity.Title. Build store.Doc (see below). s.indexer.Upsert.
    // 7. Return input (with id/uuid/timestamps populated).
}

func (s *Store) Get(ctx context.Context, kind, slug string) (map[string]any, error)
func (s *Store) List(ctx context.Context, kind string, filters map[string]any) ([]map[string]any, error)
func (s *Store) Update(ctx context.Context, kind, slug string, patch map[string]any) (map[string]any, error)
func (s *Store) Archive(ctx context.Context, kind, slug string) error
```

The doc-building logic (replaces personDoc/orgDoc/dealDoc/taskDoc/interactionDoc):
```go
func entityDoc(kind string, entity schema.Entity, input map[string]any, body string) store.Doc {
    title, _ := schema.Render(entity.Title, input)
    doc := store.Doc{
        ID: slugFromInput(input), Type: kind, Slug: slugFromInput(input),
        UUID: input["uuid"].(string), Title: title, Body: body,
    }
    for name, field := range entity.Fields {
        switch field.Type {
        case "string[]":
            if name == "tags" { doc.Tags = toStrSlice(input[name]) }
        case "link":
            if v, ok := input[name].(string); ok && v != "" { doc.LinksTo = append(doc.LinksTo, v) }
        case "link[]":
            doc.LinksTo = append(doc.LinksTo, toStrSlice(input[name])...)
        case "relation[]":
            for _, rel := range toRelSlice(input[name]) { doc.Rels = append(doc.Rels, rel.Type+":"+rel.To) }
        }
    }
    doc.LinksTo = append(doc.LinksTo, ParseLinks(body)...)
    return doc
}
```

Also port `tags.go` at this point: replace the kind-switch + five `write*Raw` helpers with a single `writeRaw(s, kind, slug, fn)` that uses `generic.go`'s read/write path.

Tests: table-driven CRUD against a fixture schema in a `t.TempDir()` store. Exercise required/unique/enum validation paths.

Recreate `merge_collision_test.go` here: two concurrent `Create` calls against the generic store should produce `-2`-suffixed slugs, both present on disk.

Commit: `feat(store): add generic schema-driven CRUD on map[string]any`

### Step 8 — Runtime cobra generator

Add:
- `runtime/cobra.go` — `BuildRootCmd(*schema.Schema, *md.Store) *cobra.Command`. Walks entities, registers `mdql <kind> add|list|show|update|archive` subtrees. Skips `update` when `append_only`, skips `archive` when `!archivable`.
- `runtime/flags.go` — `addFlags(cmd *cobra.Command, entity schema.Entity, out map[string]any)`. Maps field type → `StringVar`/`Float64Var`/`BoolVar`/`StringSliceVar`. Handles `required`, `enum` re-validation in a `PreRunE`, `default`.
- `runtime/doc.go` — houses `entityDoc` from step 7 if not already in `store/md/generic.go`.
- `runtime/format.go` — derives `format.ColumnDef` from schema fields (default: every non-array scalar, max 8 columns, no body).
- `engine/engine.go` — `Run(args []string, s *schema.Schema) int`. Opens the store, builds the cobra tree, `Execute()`, returns the exit code.

Cross-entity commands registered once:
- `mdql init` — loop `s.Entities` creating `<root>/<dir>` + `<root>/_archive/<dir>`; write `.gitignore` with `RuntimeDir/`.
- `mdql index rebuild` — `store.Reindex(ctx)`.
- `mdql search <q>` — `search.Search` (already implemented in mdql).
- `mdql wiki backlinks|orphans|dangling` — `wiki.Backlinks|Orphans|DanglingLinks` (already implemented).
- `mdql tag list|add|remove|delete|count` — generic over entities from schema.
- `mdql relate <from> <to> --type <edge>` — only if any entity declares a `relation[]` field.

Wire `cmd/mdql/main.go`:
```go
func main() {
    path := "./schema.yml"
    for i, a := range os.Args { if a == "--schema" && i+1 < len(os.Args) { path = os.Args[i+1] } }
    data, err := os.ReadFile(path)
    if err != nil { fmt.Fprintln(os.Stderr, "mdql: error:", err); os.Exit(1) }
    s, err := schema.Parse(data)
    if err != nil { fmt.Fprintln(os.Stderr, "mdql: error:", err); os.Exit(1) }
    os.Exit(engine.Run(os.Args, s))
}
```

Commit: `feat(runtime): schema-driven cobra command generator + engine.Run`

### Step 9 — Integration test + cleanup

- Add `integration_test.go` or similar in `cmd/mdql/` or a new `internal/intg/` package.
- Load the CRM-equivalent fixture schema. Exec the binary (built once in `TestMain`) against a `t.TempDir()` root.
- Seed people/orgs/deals/tasks; assert list output parity with golden JSON files.
- `go vet ./...` and `golangci-lint run` must pass.

### Steps 10–13 — crm-cli slim-down

Back in `/Users/tomfuertes/sandbox/git-repos/crm-cli`:

10. Delete `internal/`. Keep `cmd/crm/main.go`, `docs/`, `README.md`, `CHANGELOG.md`, `.goreleaser.yml`.
11. Add `schema.yml` at repo root with the five entities (full YAML in `.claude/plans/structured-booping-sedgewick.md` §A). `go.mod` gains `require github.com/notambourine/mdql v0.0.0-<sha>` plus a `replace github.com/notambourine/mdql => ../mdql` directive (user requested local iteration first — no version tag until API stabilizes).
12. Rewrite `cmd/crm/main.go`:
    ```go
    package main
    import (
        _ "embed"
        "fmt"
        "os"
        "github.com/notambourine/mdql/engine"
        "github.com/notambourine/mdql/schema"
    )
    //go:embed schema.yml
    var schemaYAML []byte
    func main() {
        s, err := schema.Parse(schemaYAML)
        if err != nil { fmt.Fprintln(os.Stderr, "crm: error:", err); os.Exit(1) }
        os.Exit(engine.Run(os.Args, s))
    }
    ```
13. Add `queries.md` at crm-cli repo root (man-page-style `| jq` recipes for pipeline/overdue/context/duplicate-email). Example seed entries (from the plan):
    - *Pipeline by stage*: `crm list deal --format json | jq 'group_by(.stage) | map({stage:.[0].stage, count:length, value:(map(.value)|add)})'`
    - *Overdue tasks*: `crm list task --completed false --format json | jq --arg now "$(date -u +%Y-%m-%dT%H:%M:%SZ)" 'map(select(.due_at < $now))'`
    - *Context briefing for a person*: composite of `show person`, `search`, `wiki backlinks`
    - *Duplicate emails*: `crm list person --format json | jq 'group_by(.email|ascii_downcase) | map(select(length>1))'`
    Also update crm-cli's README to point at mdql for CLI reference, keep schema-editing guidance local.

## Open v1.1 questions (defer)

1. `link[]` as first-class type vs `string[]` + body-wikilink parsing (v1 says first-class).
2. Default list columns — v1 shows every non-array scalar up to 8 cols; optional `list_columns:` per entity in v1.1.
3. `queries.md` placement — both: generic template in mdql, CRM-specific recipes in crm-cli.
4. Schema evolution tooling — `mdql migrate --from X --to Y` is v1.1+.
5. `unique` scan cost — O(n) per Create is acceptable for ≤10k records.
6. Module path — confirmed `github.com/notambourine/mdql`.

## Verification gates

- After step 6: `go test ./...` passes on the trimmed test corpus.
- After step 9: integration test loads fixture schema, exercises generated CLI end-to-end. Diff against pre-extraction `crm` binary output should be empty except for the four intentionally dropped commands (`status`, `context`, `log`, `deal pipeline`).
- After step 12: in crm-cli, `crm person add "Jane Smith" --email j@x` + `crm search jane` + `crm wiki backlinks jane-smith` produce identical output to the pre-extraction binary. `go vet ./...` and `golangci-lint run` must be clean.
- After step 13: validate a golden `queries.md` recipe end-to-end — seed three deals across three stages, run the pipeline aggregation pipe, assert expected group totals.

## Working directory and environment

- mdql repo: `/Users/tomfuertes/sandbox/git-repos/mdql` on branch `main`.
- crm-cli repo: `/Users/tomfuertes/sandbox/git-repos/crm-cli` on branch `markdown-db`.
- Go 1.25 (go.mod's `go` directive was bumped by `go mod tidy`).
- `mdql-extraction-staging` branch exists in crm-cli from the abandoned subtree attempt — safe to delete (`git branch -D mdql-extraction-staging`) whenever.
