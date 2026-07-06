# Workflow Collections Reference

A **workflow collection** is a user-defined directory tree under `workflow/<type>/`
in the campaign root. It groups workitems of a single type (`research`, `feature`,
`bug`, etc.) and wires them into camp's navigation and completion systems via a
shortcut key and a concept entry.

## Concept boundaries

Two unrelated things share the word "workflow" in camp. This document covers
only the workitem-collection workflow surface. The other meaning, the per-workitem
state machine driven by `camp flow` and `.workflow.yaml`, is unrelated.

| Term | Location | What it is |
|---|---|---|
| Workflow collection (this doc) | `workflow/<type>/` | User-created collection of workitems of one type, with navigation config |
| Flow status | `.workflow.yaml` per workitem | Per-workitem lifecycle state machine, owned by `camp flow` |

> `camp flow` is the low-level `.workflow.yaml` status engine. The dungeon move
> it performs (active -> dungeon/completed|archived|someday) is the primitive that
> `camp shelve` and `camp workitem promote --target {completed,archived,someday}`
> build on. It is intentionally hidden from `camp --help`: the user-facing surface
> for moving work forward is `camp promote` (and `camp intent promote` /
> `fest promote`), not `camp flow`.

A workflow collection is not:

- a workitem (a single tracked unit of work)
- an intent (a raw idea captured under `.campaign/intents/`)
- a festival (a structured multi-phase project plan)
- a project (a git submodule registered under `projects/`)

Use `camp workflow create` when you want to group a set of workitems under a
named type, navigate to them with a shortcut, and have camp surface them in tab
completion.

---

## Lifecycle walkthrough

### 1. Create

```
camp workflow create research --shortcut re --title "Research" --category research
```

Output on first run:

```
created workflow/research
  shortcut: re -> workflow/research/
  workitem type: research
  category: research
  dungeon dirs: dungeon/completed/, dungeon/archived/, dungeon/someday/
next: camp workitem create <slug> --type research
```

`--category` sets the workflow category (default `plan`). The category must
already exist under `workflows.categories` in `campaign.yaml` (shipped:
`plan`, `research`, `pipeline`, `review`); unknown categories are rejected. On
apply it writes `workflows.category_by_type.<type>`.

What `create` writes:

```
workflow/research/
├── OBEY.md
└── dungeon/
    ├── completed/
    │   └── .gitkeep
    ├── archived/
    │   └── .gitkeep
    └── someday/
        └── .gitkeep
```

It also writes two config entries:

- `.campaign/settings/jumps.yaml` — shortcut `re -> workflow/research/`
- `.campaign/campaign.yaml` — concept `research -> workflow/research/`

Rerunning with the same arguments exits 0 with `no changes for workflow research`.
Rerunning with a different shortcut key fails unless `--replace` is set.

See [camp workflow create](cli-reference/camp_workflow_create.md) for the full
flag reference.

### 2. List

```
camp workflow list
```

```
TYPE        CATEGORY  SHORTCUT  ITEMS  UPDATED
research    research  re        4      2026-05-20T14:32:00Z
feature     plan      fe        12     2026-05-22T09:01:00Z
```

Lists every user-created workflow collection. Builtin types (`intent`, `design`,
`explore`, `festival`, `code_reviews`, `pipelines`) are excluded. Entries come
from the union of concepts listed in `campaign.yaml` and directories present on
disk under `workflow/`. The workitem count is the number of `.workitem` marker
files found under the type directory.

See [camp workflow list](cli-reference/camp_workflow_list.md).

### 3. Inspect

```
camp workflow show research
```

```
workflow: research
  title: Research
  path: workflow/research/
  shortcut: re -> workflow/research/
  has_concept: true
  has_dir: true
  workitems: 4
recent:
  2026-05-20T14:32:00Z  workflow/research/rate-limiting
  2026-05-18T11:00:00Z  workflow/research/auth-design
```

See [camp workflow show](cli-reference/camp_workflow_show.md).

### 4. Add a shortcut later

If you skipped `--shortcut` at creation time, or want a second shortcut pointing
at the same collection:

```
camp workflow shortcut add research res
```

This reuses the same upsert logic as `create`. Use `--replace` if the key already
points elsewhere.

See [camp workflow shortcut add](cli-reference/camp_workflow_shortcut_add.md).

### 5. Check consistency

```
camp workflow doctor
```

Doctor is read-only. It checks the config and filesystem for inconsistencies and
reports findings with stable dotted-domain codes.

```
doctor: 2 finding(s)
  [error] workflow.shortcut.missing-target shortcut:re — shortcut "re" points to missing workflow/research/
    hint: remove the shortcut from .campaign/settings/jumps.yaml or restore the directory; auto-fix removes the shortcut
  [warning] workflow.dir.missing-concept dir:workflow/feature/ — workflow workflow/feature/ has no concept entry
    hint: auto-fix adds a concept entry derived from the directory name
```

Exit code is 0 when all findings are `info` or `warning`. Exit code 2 when any
finding has severity `error`.

See [camp workflow doctor](cli-reference/camp_workflow_doctor.md).

### 6. Repair

```
camp workflow sync
```

Sync is dry-run by default. It plans the auto-fixable subset of doctor findings
and prints what it would do. Pass `--apply` to write:

```
camp workflow sync --apply
```

```
sync: applied 2 / 2 auto-fixable findings
  fixed workflow.shortcut.missing-target (re)
  fixed workflow.dir.missing-concept (workflow/feature/)
```

See [camp workflow sync](cli-reference/camp_workflow_sync.md).

---

## Plan/apply model

`camp workflow create` separates work into two phases:

1. **Plan phase.** No writes. Checks which directories, `.gitkeep` files,
   `OBEY.md`, shortcut entry, and concept entry are missing or would change.
   Classifies each as `create`, `update`, `skip-exists`, or `no-op`.

2. **Apply phase.** Iterates the plan and writes.

`--dry-run` runs the plan phase only and exits without writing.

`camp workflow sync` uses the same model with its default inverted: dry-run is
the default and `--apply` triggers writes.

## Dungeon scaffold

`camp workflow create` writes only terminal dungeon directories under
`workflow/<type>/`. It does not create top-level `inbox`, `active`, or `ready`
buckets. Top-level `workflow/<type>/<slug>/` entries are the normal working area
for that collection, matching the existing `workflow/design` and
`workflow/explore` model.

| Directory | Purpose |
|---|---|
| `dungeon/completed/` | Finished |
| `dungeon/archived/` | Shelved, preserved |
| `dungeon/someday/` | Deprioritized |

Each directory contains a `.gitkeep` file so git tracks empty directories and
the paths are available after a clean checkout.

`camp workitem create --type <T>` currently places new items at
`workflow/<T>/<slug>/`.

`discover_custom_workflows.go` skips dot-prefixed names inside the type
directory. The `.gitkeep` files are therefore invisible to workitem discovery.
The `dungeon/` subtree is terminal storage and is not treated as active work.

### Known issues

**CW0003-workflow-03 (major).** The scaffold writes (`MkdirAll` per directory,
`WriteFile` per `.gitkeep`) are followed by separate `SaveJumpsConfig` and
`SaveCampaignConfig` calls. There is no rollback between them. A write failure
mid-apply leaves the on-disk scaffold intact but the config partially updated
(e.g. shortcut written, concept not written). `camp workflow doctor` detects
the resulting inconsistency; `camp workflow sync --apply` is the recovery path.

---

## JSON output

All subcommands accept `--json`. Output goes to stdout as a single JSON document.
The top-level `schema_version` field is `"workflow/v1"` for all responses.
Warnings from cache-invalidation failures still go to stderr as plain text.

### `create` schema

```json
{
  "schema_version": "workflow/v1",
  "generated_at": "2026-05-25T03:30:00Z",
  "type": "research",
  "title": "Research",
  "workflow_dir": "workflow/research/",
  "status_dirs": ["dungeon/completed/", "dungeon/archived/", "dungeon/someday/"],
  "obey_written": true,
  "shortcut": {"key": "re", "path": "workflow/research/", "replaced": false, "no_change": false},
  "concept": {"name": "research", "path": "workflow/research/", "replaced": false, "no_change": false},
  "replaced": [],
  "no_changes": false,
  "dry_run": false,
  "applied": true
}
```

`obey_written` is `true` only when `OBEY.md` was actually written; `false` when
it was already present. `no_changes` is `true` when every action was a no-op.
`applied` is `true` only when the command actually mutated files or campaign
configuration. Dry-runs and idempotent no-op reruns report `applied: false`.

### `list` schema (as-built)

```json
{
  "schema_version": "workflow/v1",
  "generated_at": "...",
  "workflows": [
    {
      "type": "research",
      "path": "workflow/research/",
      "shortcut": "re",
      "workitem_count": 4,
      "has_concept": true,
      "has_dir": true,
      "has_shortcut": true,
      "last_modified": "2026-05-20T14:32:00Z"
    }
  ]
}
```

### `show` schema (as-built)

```json
{
  "schema_version": "workflow/v1",
  "generated_at": "...",
  "type": "research",
  "title": "Research",
  "path": "workflow/research/",
  "shortcut": "re",
  "shortcut_path": "workflow/research/",
  "has_concept": true,
  "has_dir": true,
  "has_shortcut": true,
  "workitem_count": 4,
  "recent": [
    {"slug": "rate-limiting", "path": "workflow/research/rate-limiting", "modified": "..."}
  ]
}
```

### `doctor` schema

```json
{
  "schema_version": "workflow/v1",
  "generated_at": "...",
  "findings": [
    {
      "code": "workflow.shortcut.missing-target",
      "severity": "error",
      "target": "shortcut:re",
      "message": "shortcut \"re\" points to missing workflow/research/",
      "fix_hint": "remove the shortcut from .campaign/settings/jumps.yaml or restore the directory; auto-fix removes the shortcut",
      "auto_fixable": true
    }
  ],
  "error_count": 1,
  "warning_count": 0,
  "info_count": 0
}
```

### `sync` schema

```json
{
  "schema_version": "workflow/v1",
  "generated_at": "...",
  "findings": [...],
  "planned": [{"finding": {...}, "kind": "remove_shortcut", "target": "re"}],
  "applied": [{"finding": {...}, "kind": "remove_shortcut", "target": "re"}],
  "apply": true
}
```

The fields above are the actual v1 contract. Consumers should not assume fields
not listed here are present.

---

## Doctor finding codes

Finding codes are stable dotted-domain strings. Consumers may dispatch on them.

| Code | Severity | Trigger |
|---|---|---|
| `workflow.shortcut.missing-target` | error | Shortcut points to a non-existent `workflow/<type>/` directory |
| `workflow.concept.missing-dir` | error | Concept entry references a missing `workflow/<type>/` directory |
| `workflow.shortcut.duplicate` | error | Two shortcut keys normalize to the same value |
| `workflow.dir.missing-concept` | warning | Workflow directory exists but no concept entry references it |
| `workflow.cache.stale` | warning | Nav index `BuildTime` predates the workflow root's mtime |
| `workflow.dir.missing-shortcut` | info | Workflow directory has a concept but no shortcut |

Each finding carries `code`, `severity`, `target`, `message`, `fix_hint`, and
`auto_fixable`. Doctor sorts findings by code then by target.

`--fix` is not implemented. Passing an unknown flag errors via cobra.
Use `camp workflow sync --apply` to apply auto-fixable findings.

### Sync action kinds

`camp workflow sync` maps auto-fixable findings to action kinds:

| Finding code | Action kind |
|---|---|
| `workflow.shortcut.missing-target` | `remove_shortcut` |
| `workflow.concept.missing-dir` | `remove_concept` |
| `workflow.dir.missing-concept` | `add_concept` |
| `workflow.shortcut.duplicate` | `deduplicate_shortcut` |
| `workflow.cache.stale` | `delete_nav_cache` |

`workflow.dir.missing-shortcut` is not auto-fixable because it requires the user
to choose a shortcut key.

When deduplicating, sync keeps the normalized (lowercase) form of the key, not
the lexicographically first variant.

## Idempotency

`camp workflow create` is idempotent. Rerunning with identical arguments on an
already-created collection exits 0, writes nothing, and prints
`no changes for workflow <type>`. In JSON mode, `no_changes: true` and
`applied: false`.

Rerunning with a different shortcut key or title fails unless `--replace` is
set. A partial scaffold (some directories present, some missing) is completed on
rerun: only the missing pieces are created.

`camp workflow sync` with no arguments is always read-only and always exits 0.

---

## Builtin type exclusions

The following names are reserved and excluded from user workflow enumeration
even if a directory or concept entry exists for them:

`intent`, `design`, `explore`, `festival`, `code_reviews`, `pipelines`

---

## Related pages

- [camp workflow](cli-reference/camp_workflow.md) — command group overview
- [camp workflow create](cli-reference/camp_workflow_create.md) — flag reference
- [camp workflow list](cli-reference/camp_workflow_list.md) — flag reference
- [camp workflow show](cli-reference/camp_workflow_show.md) — flag reference
- [camp workflow shortcut add](cli-reference/camp_workflow_shortcut_add.md) — flag reference
- [camp workflow doctor](cli-reference/camp_workflow_doctor.md) — flag reference
- [camp workflow sync](cli-reference/camp_workflow_sync.md) — flag reference
- [Campaign directory reference](campaign-directory-reference.md) — `.campaign/` layout including `jumps.yaml` and `campaign.yaml`
