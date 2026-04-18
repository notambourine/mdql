# mdql by example — project + typed sub-files

This directory is a runnable README. A ~50-line [`schema.yml`](schema.yml)
defines two entities (`person`, `project`) where `project` owns four
typed sub-file kinds (`meeting`, `decision`, `milestone`, `note`).
[`run.sh`](run.sh) rebuilds the `mdql` binary, wipes the store, re-seeds
it (2 people / 1 project / 2 meetings / 1 decision / 1 milestone / 1
catchall-absorbed note), and captures the stdout+stderr of ~40 CLI
invocations as `out/NN-thing.out` goldens. Read the captured files to
see what mdql does without running anything — or run `bash run.sh` to
reproduce them locally.

## Run it

```sh
bash run.sh           # wipe + regenerate ./out/ from the fresh binary
bash run.sh --check   # regenerate into a tempdir, diff against ./out/
```

`--check` is how CI guards against silent CLI-output drift (see
`.github/workflows/ci.yml`). Exit 1 on drift, 0 when in sync.

UUID and RFC3339 timestamp values are canonicalized post-capture so
diffs compare shape, not per-run identity. (mdql itself does not
auto-inject `id`/`uuid`/`created_at` — the scrub is defense in depth.)

## Schema (at a glance)

```
person  people/
  name, email, role (ic|lead|exec), tags

project projects/
  name, stage (draft|active|done), lead → person, tags
  sub-files:
    meeting    — meetings/{{date}}-{{subject}}.md
    decision   — decisions/{{title}}.md
    milestone  — milestones/{{name}}.md
    note       — catchall: any loose .md under the project dir
```

Sub-files are typed children of a parent entity. They share the parent's
folder and show up in `project show --format json` as a `sub_files`
graph. The `note` kind is a **catchall** — no slug template, no `add`
verb. Instead, any loose `.md` sitting directly under
`projects/<slug>/` is attributed to `note` at read time. See
[`09-loose-note.out`](out/09-loose-note.out) and the final `sub_files`
entry in [`15-project-show.out`](out/15-project-show.out).

## Layout

```
example/
├── schema.yml              # single source of truth
├── run.sh                  # regenerator / --check harness
├── people/{slug}/index.md  # one folder per person
├── projects/{slug}/
│   ├── index.md            # the project's canonical frontmatter
│   ├── meetings/*.md       # typed sub-files
│   ├── decisions/*.md
│   ├── milestones/*.md
│   └── open-questions.md   # loose .md → absorbed by the `note` catchall
├── _archive/people/        # archived entities (bob-quinn)
├── .mdql/                  # bleve search index (gitignored)
└── out/                    # captured CLI outputs (committed)
```

## A tour of the goldens

Read these first to see the shape without running anything:

- [`out/15-project-show.out`](out/15-project-show.out) — **the headline**.
  `project show --format json` returns the project's own fields **plus**
  a `sub_files[]` graph of every meeting/decision/milestone/loose-note,
  each with root-relative `path` so agents can fetch the body directly.
- [`out/10-project-list-table.out`](out/10-project-list-table.out) — the
  default human-facing table, wikilinks rendered as `[[slug]]`.
- [`out/11-project-list-json.out`](out/11-project-list-json.out) — the
  pipe-ready JSON. Pair with `jq` for ad-hoc queries.
- [`out/14-project-list-quiet.out`](out/14-project-list-quiet.out) —
  `--quiet` emits one ID per line, ideal for `xargs`.
- [`out/16-meeting-list.out`](out/16-meeting-list.out) + [`out/17-meeting-show.out`](out/17-meeting-show.out)
  — sub-files have the same CRUD surface as entities, just scoped to
  their parent slug.
- [`out/40-wiki-backlinks-ana.out`](out/40-wiki-backlinks-ana.out) —
  wiki backlinks resolve through frontmatter link fields. Ana is the
  project lead (`lead: [[ana-ray]]`), so `launch-site` shows up here.
- [`out/41-wiki-orphans.out`](out/41-wiki-orphans.out) — entities with
  no inbound references.
- [`out/82-file-meeting.out`](out/82-file-meeting.out) — a sub-file on
  disk. Clean frontmatter, no auto-injected metadata.
- [`out/90-err-validation.out`](out/90-err-validation.out) (exit 2),
  [`out/91-err-notfound.out`](out/91-err-notfound.out) (exit 3),
  [`out/92-err-required.out`](out/92-err-required.out) (exit 1,
  cobra-level flag-required fires before the runtime classifier) — the
  error classes mdql surfaces with distinct exit codes.

## Agent usage notes

- Default output is TTY-sensitive: attached to a terminal it renders a
  table; piped, it emits JSON. Compare
  [`10-project-list-table.out`](out/10-project-list-table.out) (`--format table`)
  with [`11-project-list-default-pipe.out`](out/11-project-list-default-pipe.out)
  (no format flag, captured from a redirected stdout — so it came out as JSON).
- Pipe through `jq` for any aggregation mdql doesn't do itself. There
  is no built-in "list upcoming meetings" or "count by stage" — by
  design, those recipes live in `queries.md` at the repo root.
- Slug generation is deterministic: `Ana Ray` → `ana-ray`, `Launch
  Site` → `launch-site`, `2026-04-01 / Kickoff` → `2026-04-01-kickoff`.
  Pass bare slugs or the wiki-wrapped form `[[ana-ray]]` to link flags
  — both work.
- The single most useful command for an agent session is
  `mdql schema describe --format json` — it returns the full entity +
  sub-file + command graph, so the agent can derive valid invocations
  without hardcoding.

## Extending the example

Want to add a `comment` sub-file kind under `project`? Edit
`schema.yml`, add `mdql project comment add` commands to `run.sh`,
regenerate, and commit the new `out/NN-*.out` files. `run.sh --check`
in CI will then pin the shape of the new commands.
