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

## Environment

- mdql: `/Users/tomfuertes/sandbox/git-repos/mdql` on `main`.
- crm-cli source material: `/Users/tomfuertes/sandbox/git-repos/crm-cli` on `markdown-db`.
- Go 1.25.

Work items (remaining extraction steps, verification gates, source-material file references) live in TaskList — not here.
