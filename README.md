# mdql

Schema-driven typed CRUD + full-text search + wiki graph over markdown files with YAML frontmatter. One binary, one `schema.yml`, your directory of notes — no app, no daemon, no cloud.

## What it is

Define your entity types in a schema file:

```yaml
entities:
  book:
    dir: books
    title: "{{.title}}"
    fields:
      title:  {type: string, required: true}
      author: {type: link, target: person}
      rating: {type: enum, values: [abandoned, reading, done]}
      tags:   {type: string[], sorted: true}
```

Get typed CRUD, full-text search, and `[[wiki]]` backlinks for free:

```bash
mdql init
mdql book add --title "A Pattern Language" --author christopher-alexander --rating done
mdql search "pattern"
mdql wiki backlinks christopher-alexander
mdql book list --format json | jq '.[] | select(.rating == "done")'
```

## Why

Local-first, git-friendly, AI-friendly. Your data is plain markdown you can read without mdql installed. The schema drives everything — adding a new entity type means editing `schema.yml`, not writing code.

## Status

Pre-release. API and schema grammar subject to change until v0.1.0.

## Credit

Sparked by [crm-cli](https://github.com/jdanielnd/crm-cli) by @jdanielnd — a local-first terminal CRM whose storage layer showed how much leverage you get from typed markdown + Bleve + atomic writes. mdql generalizes that layer so it works for any domain, not just personal CRM.

## License

MIT. See [LICENSE](LICENSE).
