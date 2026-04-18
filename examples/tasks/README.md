# examples/tasks — tasks + projects + people

Flat task tracker. `task` is a first-class entity (not a sub-file)
because tasks have independent lifecycle, assignees, and cross-project
queries. `comment` is a task sub-file so discussion lives next to the
task it's about.

## Schema (at a glance)

```
person   people/     name, email
project  projects/   name, status (planned|active|shipped|cancelled)
task     tasks/      title, status (todo|doing|done|blocked), priority (p0..p3),
                     project → project, assignee → person, due, tags
  sub-files:
    comment — tasks/{{slug}}/comments/{{date}}-{{author}}.md
```

## Three commands that demo it

```sh
# 1. Dry-run a status change before committing. Emits a WritePlan, no disk write.
mdql --root . --schema ./schema.yml task update ship-website \
  --status done --dry-run --format json

# 2. Field projection. Agent-friendly: only the keys you asked for.
mdql --root . --schema ./schema.yml task list \
  --fields id,title,status,priority --format json | jq '.[] | select(.status == "todo")'

# 3. Sub-file search. Find comments containing a term across every task.
mdql --root . --schema ./schema.yml search "legal" --sub comment --format json
```

## Pairs nicely with `jq`

mdql doesn't build aggregation into the CLI — you pipe JSON through `jq`.

```sh
# Count open tasks by priority.
mdql --root . --schema ./schema.yml task list --format json \
  | jq -r 'map(select(.status != "done")) | group_by(.priority) | map({(.[0].priority): length}) | add'

# Overdue: due date < today.
mdql --root . --schema ./schema.yml task list --format json \
  | jq --arg today "$(date +%F)" '.[] | select(.due != null and .due < $today and .status != "done")'
```
