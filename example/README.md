# mdql by example — a tiny issue tracker

This directory is a runnable README. A 37-line [`schema.yml`](schema.yml)
defines three entities (project, person, issue). [`run.sh`](run.sh)
rebuilds the `mdql` binary, wipes the store, re-seeds it with three
people / one project / three issues, and captures the stdout+stderr of
~35 CLI invocations as `out/NN-thing.out` goldens. You can read the
captured files to see what mdql does without running anything — or run
`bash run.sh` to reproduce them locally.

## Run it

```sh
bash run.sh           # wipe + regenerate ./out/ from the fresh binary
bash run.sh --check   # regenerate into a tempdir, diff against ./out/
```

`--check` is how CI guards against silent CLI-output drift (see
`.github/workflows/ci.yml`). Exit 1 on drift, 0 when in sync.

UUID and RFC3339 timestamp values are canonicalized post-capture so
diffs compare shape, not per-run identity.

## Schema (at a glance)

Three entities, all slugged from their own display field:

| entity  | dir         | title template | link fields                  |
|---------|-------------|----------------|------------------------------|
| project | `projects/` | `{{.name}}`    | `owner` → person             |
| person  | `people/`   | `{{.name}}`    | —                            |
| issue   | `issues/`   | `{{.title}}`   | `project` → project (required), `assignee` → person |

Default values: `project.status` defaults to `active`, `issue.priority`
to `medium`, `issue.open` to `true`. All three defaults are applied
inside `store.Create`, so you can `mdql project add --name X` without
`--status`.

## Layout

```
example/
├── schema.yml              # the single source of truth
├── run.sh                  # regenerator / --check harness
├── people/                 # person entity files
├── projects/               # project entity files
├── issues/                 # active issue entity files
├── _archive/issues/        # archived issues (add-analytics)
├── .mdql/                  # bleve search index (gitignored)
└── out/                    # captured CLI outputs (committed)
```

## A tour of the goldens

Read these first to see mdql's shape without running it:

- [`out/10-issue-list-table.out`](out/10-issue-list-table.out) — the
  default human-facing table, wikilinks rendered as `[[slug]]`.
- [`out/11-issue-list-json.out`](out/11-issue-list-json.out) — the
  pipe-ready JSON with every field. Pair this with `jq` for ad-hoc
  queries: `mdql --format json issue list | jq '[.[] | select(.open)]'`.
- [`out/14-issue-list-quiet.out`](out/14-issue-list-quiet.out) —
  `--quiet` emits one ID per line, ideal for `xargs`.
- [`out/40-wiki-backlinks-ana.out`](out/40-wiki-backlinks-ana.out) —
  wiki backlinks resolve across both frontmatter link fields and body
  `[[slug]]` references. The project `launch-site` has `owner:
  [[ana-ray]]` in frontmatter, which is why it appears here.
- [`out/41-wiki-orphans.out`](out/41-wiki-orphans.out) — entities with
  no inbound references. Every issue is an orphan here: issues link
  outward to `launch-site`, but nothing links back to them.
- [`out/43-wiki-backlinks-launch-site.out`](out/43-wiki-backlinks-launch-site.out)
  — all three issues, found via their frontmatter `project:
  [[launch-site]]` pointer. The frontmatter-link path is a recently
  fixed regression.
- [`out/82-file-issue.out`](out/82-file-issue.out) — the on-disk markdown
  shape. Links are stored as YAML-quoted `'[[slug]]'`, UUID and
  timestamps are stamped at create time.
- [`out/90-err-validation.out`](out/90-err-validation.out),
  [`out/91-err-notfound.out`](out/91-err-notfound.out),
  [`out/92-err-required.out`](out/92-err-required.out) — the three
  error classes mdql surfaces with distinct exit codes (2, 3, 2).

## Agent usage notes

- Default output is TTY-sensitive: attached to a terminal it renders a
  table; piped, it emits JSON. Compare [`10-issue-list-table.out`]
  (set via `--format table`) with
  [`11-issue-list-default-pipe.out`] (no format flag, captured from a
  redirected stdout — so it came out as JSON).
- Pipe through `jq` for any aggregation mdql doesn't do itself. There
  is no built-in "list overdue issues" or "count by assignee" — by
  design, those recipes live in `queries.md` at the repo root.
- Slug generation is deterministic: `Ana Ray` → `ana-ray`, `Launch
  Site` → `launch-site`. Pass bare slugs or the wiki-wrapped form
  `[[ana-ray]]` to link flags — both work.

## Extending the example

Want to add a `comment` entity (append-only, linked to issue)? Edit
`schema.yml`, add `mdql comment add` commands to `run.sh`, regenerate,
and commit the new `out/NN-*.out` files. `run.sh --check` in CI will
then pin the shape of the new commands.
