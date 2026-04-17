#!/usr/bin/env bash
# example/run.sh — regenerate ./out/ goldens (default) or --check them.
#
# Default mode wipes the working store (people/ projects/ issues/
# _archive/ .mdql/ out/) and re-seeds it by running ~35 mdql commands
# against a freshly built binary, capturing stdout+stderr to out/NN-*.out.
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
    rm -rf "$ROOT/people" "$ROOT/projects" "$ROOT/issues" \
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

capture 02-person-add-ana.out --format json person add --name "Ana Ray"   --email ana@example.com
capture 03-person-add-bob.out --format json person add --name "Bob Quinn" --email bob@example.com

capture 04-project-add.out    --format json project add --name "Launch Site" --owner "[[ana-ray]]"

capture 05-issue-add-1.out --format json issue add \
    --title "Fix nav overflow"   --project "[[launch-site]]" \
    --assignee "[[bob-quinn]]"   --priority high --points 5
capture 06-issue-add-2.out --format json issue add \
    --title "Write landing copy" --project "[[launch-site]]" \
    --assignee "[[ana-ray]]"     --priority high --points 5
capture 07-issue-add-3.out --format json issue add \
    --title "Add analytics"      --project "[[launch-site]]" \
    --priority medium --points 3 --body "Owners: [[ana-ray]]"

# ── list ─────────────────────────────────────────────────────────────
capture 10-issue-list-table.out         --format table issue list
capture 11-issue-list-default-pipe.out                 issue list
capture 11-issue-list-json.out          --format json  issue list
capture 12-issue-list-csv.out           --format csv   issue list
capture 13-issue-list-tsv.out           --format tsv   issue list
capture 14-issue-list-quiet.out         --quiet        issue list

capture 15-issue-show.out               --format json  issue show fix-nav-overflow

# ── update ───────────────────────────────────────────────────────────
capture 20-issue-update.out             --format json  issue update fix-nav-overflow --priority medium
capture 21-issue-show-updated.out       --format json  issue show   fix-nav-overflow

# ── search ───────────────────────────────────────────────────────────
capture 30-search-landing.out           --format json  search landing
capture 31-search-type-filter.out       --format json  search landing --type issue

# ── wiki (40- bug 2 fix now surfaces issue refs for launch-site) ─────
capture 40-wiki-backlinks-ana.out         --format json wiki backlinks ana-ray
capture 41-wiki-orphans.out               --format json wiki orphans
capture 42-wiki-dangling.out              --format json wiki dangling
capture 43-wiki-backlinks-launch-site.out --format json wiki backlinks launch-site

# ── tag ──────────────────────────────────────────────────────────────
capture 50-tag-list.out                 --format json  tag list

# ── archive ──────────────────────────────────────────────────────────
capture 60-issue-archive.out            issue archive add-analytics
capture 61-issue-list-after-archive.out --format json issue list

# ── index ────────────────────────────────────────────────────────────
capture 62-index-rebuild.out            index rebuild

# ── help ─────────────────────────────────────────────────────────────
capture 70-help.out                     --help
capture 71-issue-help.out               issue --help
capture 72-issue-add-help.out           issue add --help

# ── on-disk shape ────────────────────────────────────────────────────
# Sprawl layout: {dir}/{slug}/index.md is the canonical frontmatter file;
# sibling files (meetings, decisions, notes) can live alongside it.
capture_file 80-file-person-ana.out people/ana-ray/index.md
capture_file 81-file-project.out    projects/launch-site/index.md
capture_file 82-file-issue.out      issues/write-landing-copy/index.md

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
} > "$OUT/83-tree.out"

# ── errors (92- repurposed after bug 1 fix: --project is required with
#    no default, so omitting it still produces the required-flag error)
capture 90-err-validation.out  issue update fix-nav-overflow --priority nope
capture 91-err-notfound.out    issue show  ghost
capture 92-err-required.out    issue add   --title "No Project"

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
