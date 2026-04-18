# mdql

Schema-driven typed CRUD + full-text search + wiki graph over markdown files with YAML frontmatter. One binary, one `schema.yml`, a directory of notes. Extracted from `notambourine/crm-cli` as a clean hat-tip (fresh git history, not a fork).

## Design principles

- **Token-respectful output**. mdql is primarily consumed by agents. Keep `--help`, error messages, lint output, and `list`/`show` rendering terse. No preamble, no ASCII art, no duplicated column headers. One line per flag. Prefer `--format json` for agent paths. The agent token budget is a user-facing concern — treat verbose output as a bug.
- **Schema is the single source of truth**. Never hardcode entity kinds; iterate `schema.Entities`. Never maintain a parallel `Dirs`/`Kind*` list.
- **Generic over typed**. One `Create(kind, map[string]any)` beats five `CreatePerson/CreateOrg/…`.
- **Pipe over aggregate**. No in-code pipeline/overdue/context logic — users pipe `list --format json` through `jq`. Recipes live in `queries.md` (consumer-side).

## Settled decisions

1. Clean hat-tip, not fork. LICENSE + README credit `jdanielnd/crm-cli`.
2. Name: `mdql` (mdgraph/mdbase/mdschema are taken).
3. Schema format: YAML (reuses `gopkg.in/yaml.v3`).
4. Drop CRM-only behaviors: no pipeline aggregation, no context briefing, no overdue filter, no duplicate-email logic in code.
5. Module path: `github.com/notambourine/mdql`.
6. **Filename is canonical id.** mdql does not auto-inject `id`, `uuid`, `created_at`, or `updated_at` into frontmatter. The slug is derived from the filename on read (injected into the in-memory record as `id`). User-declared timestamp fields are honored as normal data; mdql doesn't manage them. Slug collisions get suffixed at the filename level (`jane-smith`, `jane-smith-2`).
7. **`mdql schema describe --format json` is the agent session-entry payload.** Single command emits the full entity/field/command graph an LLM needs to derive valid commands. Stable JSON shape — integration test pins it. Always JSON; `--format` is ignored.
8. **`wiki dangling` and `wiki orphans` return `[]` not `null` on empty result** — `jq 'length'` works without nil-handling.
9. **Wiki link grammar is three forms.** `[[slug]]` (entity), `[[kind/slug]]` (kind-qualified entity), `[[parent-slug/sub-dir/sub-slug]]` (sub-file path — uses SubFile.Dir, not the sub-kind name). In-frontmatter link `target:` uses `.` for sub-file kinds (`project.decision`); in-body refs use `/` with sub-file Dirs. `ExpandLinkKeys` fans multi-segment refs so `backlinks <parent>` surfaces everything pointing into the parent, full-form and sub-file-grain alike.
10. **Sub-files index as first-class bleve docs.** Doc.Type = sub-kind (so `search --type meeting` hits sub-files directly); ParentKind/ParentSlug/SubKind carry the rest. Sub-file LinksTo always includes the parent slug, keeping parents reachable even when a sub-file only links to siblings. Entity archive cascades: removing a parent deindexes its sub-files.

## Environment

- mdql: `/Users/tomfuertes/sandbox/git-repos/mdql` on `main`.

Work items (remaining extraction steps, verification gates, source-material file references) live in TaskList — not here.
