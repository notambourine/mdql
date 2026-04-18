# examples/zettelkasten — notes + sources

A Luhmann-style zettelkasten. `note` entities carry ideas; `source`
entities are the books/articles/talks those ideas come from. Links are
the product: every note cites a source via `source: [[...]]` and refers
to sibling notes via inline `[[slug]]` in the body.

## Schema (at a glance)

```
note    notes/       title, tags, source → source, status (fleeting|literature|permanent)
source  sources/     title, author, kind (book|article|talk|other), year, tags
```

## Three commands that demo it

```sh
# 1. The graph. Who cites this source?
mdql --root . --schema ./schema.yml wiki backlinks pattern-language --format json

# 2. Notes with no inbound references — candidates for pruning or linking.
mdql --root . --schema ./schema.yml wiki orphans --format json

# 3. Broken links: [[slug]] references that don't resolve.
mdql --root . --schema ./schema.yml wiki dangling --format json
```

## Seed notes

Pre-seeded so commands return non-empty graphs on first run:

- `sources/pattern-language/index.md` — Alexander, *A Pattern Language* (book)
- `sources/notes-on-synthesis/index.md` — Alexander, *Notes on the Synthesis of Form*
- `notes/affordance-is-a-pattern/index.md` — permanent; cites `pattern-language`, links to `quality-without-a-name`
- `notes/quality-without-a-name/index.md` — permanent; cites `pattern-language`
- `notes/forces-resolve-form/index.md` — literature; cites `notes-on-synthesis`
- `notes/orphaned-idea/index.md` — fleeting; no inbound refs (demonstrates `wiki orphans`)
