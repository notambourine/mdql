#!/usr/bin/env bash
# example/run.sh — regenerate ./out/ goldens (default) or --check them.
#
# Default mode wipes the working store (people/ projects/ _archive/ .mdql/
# out/) and re-seeds it by running ~35 mdql commands against a freshly
# built binary, capturing stdout+stderr to out/NN-*.out.
#
# --check mode regenerates into a tempdir and diffs the output against
# the committed ./out/. Exits 1 on drift (for CI), 0 when in sync.
#
# UUID and RFC3339 timestamp values are canonicalized post-capture so
# goldens stay stable across runs. Commands use bash arrays rather than
# shell-split strings so zsh users get the same word-splitting as CI.

set -euo pipefail

cd "$(dirname "$0")"
EXAMPLE_DIR="$PWD"

MODE="regenerate"
if [[ "${1:-}" == "--check" ]]; then
    MODE="check"
elif [[ -n "${1:-}" ]]; then
    echo "usage: $0 [--check]" >&2
    exit 2
fi

# Build the CLI from the repo root into a tempdir so a stale binary on
# $PATH can't affect the goldens.
BIN_DIR="$(mktemp -d)"
trap 'rm -rf "$BIN_DIR" "${CHECK_DIR:-}"' EXIT
BIN="$BIN_DIR/mdql"
( cd .. && go build -o "$BIN" ./cmd/mdql )

# Resolve working root and out directory.
if [[ "$MODE" == "check" ]]; then
    CHECK_DIR="$(mktemp -d)"
    cp schema.yml "$CHECK_DIR/schema.yml"
    ROOT="$CHECK_DIR"
    OUT="$CHECK_DIR/out"
    mkdir -p "$OUT"
else
    ROOT="$EXAMPLE_DIR"
    OUT="$EXAMPLE_DIR/out"
    rm -rf "$ROOT/people" "$ROOT/projects" \
           "$ROOT/_archive" "$ROOT/.mdql" "$OUT"
    mkdir -p "$OUT"
fi

MDQL=("$BIN" --root "$ROOT" --schema "$ROOT/schema.yml")

# scrub canonicalizes run-variable fields (UUID, RFC3339 timestamps) so
# diffs compare shape, not per-run identity.
scrub() {
    sed -E \
        -e 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/00000000-0000-0000-0000-000000000000/g' \
        -e 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/2026-01-01T00:00:00Z/g'
}

capture() {
    local name="$1"; shift
    "${MDQL[@]}" "$@" 2>&1 | scrub > "$OUT/$name" || true
}

capture_file() {
    local name="$1" src="$2"
    scrub < "$ROOT/$src" > "$OUT/$name"
}

# ── seed ─────────────────────────────────────────────────────────────
capture 01-init.out init

capture 02-person-add-ana.out --format json person add --name "Ana Ray"   --email ana@example.com --role lead
capture 03-person-add-bob.out --format json person add --name "Bob Quinn" --email bob@example.com

capture 04-project-add.out    --format json project add --name "Launch Site" --lead "[[ana-ray]]" --stage active

# ── sub-files: meetings / decisions / milestones / notes ─────────────
capture 05-meeting-add-1.out --format json project meeting add launch-site \
    --date 2026-04-01 --subject "Kickoff"
capture 06-meeting-add-2.out --format json project meeting add launch-site \
    --date 2026-04-08 --subject "Copy Review"

capture 07-decision-add.out  --format json project decision add launch-site \
    --title "Use Sprawl Layout"
capture 08-milestone-add.out --format json project milestone add launch-site \
    --name "Beta Launch" --due 2026-05-15

# Catchall demo: a loose .md file sitting directly under the project dir
# is attributed to the `note` kind by the graph (schema-defined dir doesn't
# matter — any untyped .md under the project folder is absorbed).
cat > "$ROOT/projects/launch-site/open-questions.md" <<'EOF'
---
title: Open Questions
---
Will we need an SSO provider before beta?
EOF
capture_file 09-loose-note.out projects/launch-site/open-questions.md

# ── list ─────────────────────────────────────────────────────────────
capture 10-project-list-table.out         --format table project list
capture 11-project-list-default-pipe.out                 project list
capture 11-project-list-json.out          --format json  project list
capture 12-project-list-csv.out           --format csv   project list
capture 13-project-list-tsv.out           --format tsv   project list
capture 14-project-list-quiet.out         --quiet        project list

# headline: project show now returns the sub-file graph alongside fields
capture 15-project-show.out               --format json  project show launch-site

capture 16-meeting-list.out               --format json  project meeting list launch-site
capture 17-meeting-show.out               --format json  project meeting show launch-site 2026-04-01-kickoff

# ── update ───────────────────────────────────────────────────────────
capture 20-project-update.out             --format json  project update launch-site --stage done
capture 21-project-show-updated.out       --format json  project show   launch-site
capture 22-meeting-update.out             --format json  project meeting update launch-site 2026-04-08-copy-review --subject "Copy Review v2"

# ── search ───────────────────────────────────────────────────────────
capture 30-search-launch.out              --format json  search launch
capture 31-search-type-filter.out         --format json  search launch --type project

# ── wiki ─────────────────────────────────────────────────────────────
capture 40-wiki-backlinks-ana.out         --format json wiki backlinks ana-ray
capture 41-wiki-orphans.out               --format json wiki orphans
capture 42-wiki-dangling.out              --format json wiki dangling
capture 43-wiki-backlinks-launch-site.out --format json wiki backlinks launch-site

# ── tag ──────────────────────────────────────────────────────────────
capture 50-tag-list.out                   --format json tag list

# ── lint ─────────────────────────────────────────────────────────────
# 55 = agent-facing JSON; 56 = one-line summary for CI. Example store
# has one catchall-absorbed loose note → INFO only → exit 0.
capture 55-lint.out                       --format json lint
capture 56-lint-quiet.out                 --quiet       lint

# ── archive ──────────────────────────────────────────────────────────
capture 60-person-archive.out             person archive bob-quinn
capture 61-person-list-after-archive.out  --format json person list

# ── index ────────────────────────────────────────────────────────────
capture 62-index-rebuild.out              index rebuild

# ── help ─────────────────────────────────────────────────────────────
capture 70-help.out                       --help
capture 71-project-help.out               project --help
capture 72-project-add-help.out           project add --help
capture 73-project-meeting-help.out       project meeting --help
capture 74-project-meeting-add-help.out   project meeting add --help

# ── on-disk shape ────────────────────────────────────────────────────
# Sprawl layout: {dir}/{slug}/index.md is the canonical frontmatter file;
# typed sub-files live in {dir}/{slug}/{subdir}/{sub-slug}.md.
capture_file 80-file-person-ana.out people/ana-ray/index.md
capture_file 81-file-project.out    projects/launch-site/index.md
capture_file 82-file-meeting.out    projects/launch-site/meetings/2026-04-01-kickoff.md
capture_file 83-file-milestone.out  projects/launch-site/milestones/beta-launch.md

# tree — on-disk store shape (schema + entity dirs + archive). Excludes
# runtime index, goldens, harness files so the same tree renders in both
# regenerate and --check modes.
{
    echo "---tree---"
    ( cd "$ROOT" && find . -type f \
        -not -path './.mdql/*' \
        -not -path './out/*' \
        -not -name 'run.sh' \
        -not -name 'README.md' \
        -not -name '.gitignore' \
        -not -name '.DS_Store' \
        | sort )
} > "$OUT/84-tree.out"

# ── errors ───────────────────────────────────────────────────────────
# validation: enum field rejects unknown value
# notfound:   show on an unknown slug
# required:   sub-file add without a required flag
capture 90-err-validation.out  project update launch-site --stage nope
capture 91-err-notfound.out    project show  ghost
capture 92-err-required.out    project meeting add launch-site --date 2026-04-15

# ── check mode: diff $OUT against the committed ./out/ ───────────────
if [[ "$MODE" == "check" ]]; then
    if diff -ru "$EXAMPLE_DIR/out" "$OUT" > "$BIN_DIR/diff.out"; then
        echo "example/out/: in sync"
        exit 0
    else
        echo "example/out/: drift detected" >&2
        cat "$BIN_DIR/diff.out" >&2
        exit 1
    fi
fi

echo "regenerated $(ls "$OUT" | wc -l | tr -d ' ') goldens under $OUT"
