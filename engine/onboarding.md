# mdql: no schema.yml in this directory

mdql is a schema-driven CRUD + full-text search + wiki-graph layer over
markdown files with YAML frontmatter. One binary, one `schema.yml`, a
directory of notes. The schema defines entities, fields, types, and
sub-files; the CLI surface is generated from that schema at startup.

This output is what you get when no schema is present. Read it
end-to-end — it's the full feature map. When you're done, either propose
a schema for this directory (see "Bootstrapping" at the bottom) or copy
one of the bundled examples.

## Consumer context first

Schema defines what fields exist. It does not define the conventions
for using them: which enum value fits a given scenario, who goes in
which field, what the body prose means versus the frontmatter array.
That layer is the user's, and mdql cannot enforce it.

Before editing any entity, read two files:

1. `<store>/CLAUDE.md` (if present) — the store's own conventions,
   written by the user. Enum glosses ("slack vs dm: slack is a relayed
   message, dm is a direct 1:1"), role/field splits ("people[] is bare
   names; roles live in the body ## Team table"), and lifecycle rules
   ("stage flips only on handoff delivered") live here, not in schema.
2. The target entity's `index.md` body — for sprawl entities, the body
   often carries prose context (## Team tables, ## Lessons learned,
   ## Notes) that the frontmatter can't express. Read it before
   writing, and preserve it on update.

If either is missing or ambiguous, ask the user rather than guessing.
The schema will accept a write that violates the user's conventions.

## Bundled examples

Three runnable schemas ship in the mdql source tree under `examples/`:

- `examples/crm/` — person + project + sub-files (meeting/decision/
  milestone/note catchall). Closest to a team CRM.
- `examples/zettelkasten/` — note + source. Dense `[[wiki]]` linking,
  backlinks / orphans / dangling queries.
- `examples/tasks/` — task + project + person with `task.comment`
  sub-files. Shows `--fields` projection and `--dry-run` plans.

Each directory has a README and seed data so demo commands return
non-empty output on first run.

## Type grammar

A schema entry looks like:

```yaml
entities:
  note:
    dir: notes                    # where {slug}/index.md lives
    title: "{{.title}}"           # template rendered from fields
    slug:  "{{.title}}"           # slug source — filename is canonical id
    fields:
      title:  {type: string, required: true}
      tags:   {type: "string[]", sorted: true}
      status: {type: enum, values: [fleeting, literature, permanent], default: fleeting}
      source: {type: link, target: source}
      email:  {type: string, unique: true}
```

Field types:

| Type | Example | Notes |
|---|---|---|
| `string` | `name: "Ana Ray"` | unique:true enforces uniqueness store-wide |
| `string[]` | `tags: [arch, design]` | sorted:true normalizes order on write |
| `enum` | `status: active` | values[] required; default optional |
| `link` | `lead: "[[ana-ray]]"` | target:<kind> — renders `[[slug]]` in frontmatter, participates in wiki graph |

Field options: `required`, `default`, `unique`, `sorted` (arrays),
`values` (enum). Defaults fire on add when the flag is omitted.

## Wiki link grammar — three forms

```
[[ana-ray]]                              entity slug (kind resolved via link target)
[[person/ana-ray]]                       kind-qualified entity
[[launch-site/meetings/2026-04-01-kickoff]]   sub-file path (parent-slug/sub-dir/sub-slug)
```

In-frontmatter link `target:` uses `.` for sub-file kinds
(`project.decision`). In-body refs use `/` with sub-file dirs.
`wiki backlinks <parent>` fans multi-segment refs and surfaces
everything pointing into the parent, full-form and sub-file grain alike.

## Command surface (generated from schema)

```
mdql init                                scaffold dirs from schema, seed .gitignore
mdql <kind> add --<field> <val> ...      create entity (omit required → error)
mdql <kind> add ... --dry-run            returns WritePlan, no disk write
mdql <kind> list [--tag X] [--limit N]   list entities (JSON when piped)
mdql <kind> list --fields id,title       project to named keys
mdql <kind> show <slug>                  show entity (includes sub_files[] graph)
mdql <kind> update <slug> --<field> ...  update, optional --dry-run
mdql <kind> archive <slug>               soft delete to _archive/
mdql <kind> <subkind> add <parent-slug> --<field> ...    create sub-file
mdql <kind> <subkind> list <parent-slug>                 list sub-files for parent
mdql <kind> <subkind> show <parent> <sub-slug>           show one sub-file
mdql <kind> <subkind> delete <parent> <sub-slug>         delete sub-file

mdql search <query>                      full-text over titles + bodies
mdql search <query> --type meeting       filter by entity or sub-file kind
mdql search <query> --sub comment        filter by sub-file kind
mdql search <query> --kind task          filter sub-files by parent kind

mdql wiki backlinks <slug>               what links to this? (links TO array)
mdql wiki orphans                        entities with zero inbound refs
mdql wiki dangling                       [[slug]] refs that don't resolve

mdql tag list | count <t> | add <slug> <t> | remove <slug> <t> | delete <t>

mdql index rebuild                       force-regenerate bleve index from disk
mdql lint                                audit store for schema/disk drift
mdql schema describe --format json       full entity + field + command graph
```

Global flags: `--root <dir>`, `--schema <path>`, `--format table|json|csv|tsv`,
`--quiet` (IDs only), `--no-sync` (skip auto-rebuild check).

## Index auto-sync

The bleve search index lives at `<runtime_dir>/index/` (default `.mdql/index/`)
and is **derived state**, not source of truth. Every mdql command walks
the store before serving: if any `.md` file is newer than the last
reindex stamp (`<runtime_dir>/sync.json`), the index is rebuilt
automatically. This handles `git pull`, hand-edits, and fresh clones
without manual intervention.

For tight scripting loops where the walk cost dominates, pass
`--no-sync` to bypass the check (you own consistency in that case).
`mdql index rebuild` remains the force-rebuild escape hatch for
mapping changes or index corruption.

## Output shape

Default rendering is TTY-sensitive: attached to a terminal it emits a
table; piped, it emits JSON. Force JSON with `--format json`. Most
agent paths want JSON unconditionally.

```sh
# list JSON piped through jq
mdql task list --format json | jq '.[] | select(.status == "doing")'

# one ID per line for xargs
mdql project list --quiet | xargs -I% mdql project show %
```

## jq recipes

The patterns every agent session re-derives. Pin these.

```sh
# reverse lookup: which entities have X in an array field
mdql <kind> list --format json \
  | jq -r '.[] | select(.<array-field>[]? == "X") | .slug'

# projection: specific fields (fast eyeballing)
mdql <kind> list --format tsv --fields slug,<f1>,<f2>

# schema inspection: frontmatter_fields is a map, iterate with to_entries
mdql schema describe --format json \
  | jq '.entities.<kind>.frontmatter_fields | to_entries[] | {name: .key, type: .value.type}'

# flat deduped list of every value of an array field across all entities
mdql <kind> list --format json | jq -r '.[].<array-field>[]?' | sort -u

# filter by enum value
mdql <kind> list --format json | jq '.[] | select(.<enum-field> == "<value>")'
```

**TSV caveat:** TSV flattens `string[]` with space separators, so
`people: ["Tom A.", "Lulu"]` becomes `Tom A. Lulu` — ambiguous for
multi-word values. Use `--format json` when value boundaries matter.

## `--dry-run` plans

Write verbs (`add`, `update`, `delete`) accept `--dry-run` and return a
`WritePlan` instead of touching disk:

```json
{
  "action": "update",
  "kind": "task",
  "slug": "ship-beta",
  "path": "tasks/ship-beta/index.md",
  "frontmatter": { "...": "next state" },
  "before": { "frontmatter": { "...": "current state" }, "body": "..." }
}
```

`before` is absent on `add`. Paths are root-relative.

## Body input: inline, file, or stdin

Every write that accepts `--body` also accepts `--body-file <path>`
(use `-` for stdin). The two flags are mutually exclusive. Use a file
or stdin for any body that contains newlines, backticks, or quotes —
inline shell quoting fails on email bodies and transcripts.

```sh
# email body fetched via MCP, dumped to disk first
mdql project meeting add launch-site \
  --subject "review" --date 2026-04-18 --body-file /tmp/email.md

# transcript piped from another tool
my-transcript-tool --id abc | \
  mdql project meeting add launch-site \
  --subject kickoff --date 2026-04-18 --body-file -
```

## Session-entry payload for agents

Run once at the start of any mdql session:

```sh
mdql schema describe --format json
```

Returns the full entity + field + sub-file + command graph. Stable
shape — pinned by integration test. Derive valid command invocations
from it rather than hardcoding.

## Principles

- **Filename is canonical id.** mdql does not auto-inject `id`, `uuid`,
  `created_at`, or `updated_at`. The slug is derived from the filename
  on read. User-declared timestamp fields are honored as normal data.
- **Pipe over aggregate.** No built-in pipeline/overdue/by-status. Pipe
  `list --format json` through `jq` for anything mdql doesn't do.
- **Schema is the single source of truth.** To add an entity kind or
  field, edit `schema.yml` — no code change.
- **Token-respectful output.** Terse help, terse errors, terse list
  rows. Verbose output is a bug for the agent audience.

## Bootstrapping this directory

You are reading this because `./schema.yml` is missing. Do not
auto-scaffold. Instead:

1. **Explore.** Walk the current directory. What markdown files exist
   today? Read 3–5 and union their frontmatter keys. Note directories
   that already group files by kind (e.g. `people/`, `notes/`,
   `projects/launch-site/meetings/`).
2. **Sniff types.** For each frontmatter key, what does the value look
   like across files? Consistent short string with ≤8 distinct values →
   `enum`. Looks like `[[slug]]` → `link`. Arrays of short strings →
   `string[]`. Free text → `string`.
3. **Sniff sub-files.** Does a directory contain an `index.md` plus
   sibling `.md` files or sub-directories? That's the `parent/sub-file`
   shape — model the parent as an entity and the children as `files:`.
4. **Interview if unclear.** If the repo is empty or ambiguous, ask the
   user: what kinds of things are they tracking? What relationships
   matter? What filters do they want? Do not guess.
5. **Propose, don't impose.** Emit a candidate `schema.yml` for review.
   Mention trade-offs: link targets vs free strings, enum vs open set,
   sub-file vs separate entity. Let the user edit before writing to
   disk.
6. **Verify.** Once schema.yml exists, `mdql init` to scaffold dirs,
   then `mdql schema describe --format json` to confirm the full
   command tree loads.

If the user just wants to copy a bundled example: point them at
`examples/crm/`, `examples/zettelkasten/`, or `examples/tasks/` in the
mdql source tree.
