# examples/

Each subdirectory is a self-contained mdql store: `schema.yml`, seed
notes, and a README showing the three commands that best demo it. The
goal is to let you read one small schema and see the domain shape
without running anything.

| Example | Domain | Demonstrates |
|---|---|---|
| [crm/](crm/) | People + projects + meetings/decisions/milestones | Sub-files, goldens harness, `project show --format json` |
| [zettelkasten/](zettelkasten/) | Notes + sources | Dense `[[wiki]]` linking, `wiki backlinks`/`orphans`/`dangling` |
| [tasks/](tasks/) | Tasks + projects + people | `--fields` projection, `--dry-run` previews, `task.comment` sub-files |

Only `crm/` has a `run.sh` golden harness — it's the CI pin. The other
two are read-first designs: inspect `schema.yml` + the seed notes, then
run commands against them from inside the dir.

## Running an example locally

```sh
cd examples/zettelkasten
mdql --root . --schema ./schema.yml schema describe --format json
mdql --root . --schema ./schema.yml search "your query"
```

Or set a per-shell alias:

```sh
alias zk='mdql --root ~/sandbox/git-repos/mdql/examples/zettelkasten --schema ~/sandbox/git-repos/mdql/examples/zettelkasten/schema.yml'
```
