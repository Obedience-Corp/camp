# `camp workflow` command surface — design

Sequence: `001_IMPLEMENT/01_custom_workflow_creation`
Festival: `camp-workitem-linking-and-commits-CW0003`
Base PR: #298 — `Create custom workflow collections` (commit `29f8c29`, merged 2026-05-20)

This document is the contract this sequence implements. It defines the
post-PR-#298 audit, the gap analysis, and the design of every new flag and
subcommand before any production code is written. It is consumed by subsequent
implementation tasks in this sequence.

---

## 1. PR #298 audit

What PR #298 shipped (verified by reading source on this branch):

| File | LOC | Role |
|---|---:|---|
| `internal/commands/workflow/workflow.go` | 18 | Parent command, registers `create`. |
| `internal/commands/workflow/create.go` | 215 | `camp workflow create <type>` core. |
| `internal/commands/workflow/create_test.go` | 433 | Unit coverage. |
| `internal/nav/configured_navigation.go` | 341 | Custom-shortcut routing for `camp go <shortcut> ...` and drill forms (shortcut-drill, slash-drill, concept, path-alias). |
| `internal/pathsafe/segment.go` | 37 | Single source of path-segment validation. |
| `internal/workitem/discover_custom_workflows.go` | 81 | Discovers `workflow/<type>/<slug>/.workitem` for non-builtin types. |
| `internal/workitem/discover_custom.go` | 27 | `builtinTypes` set; marker-presence check. |
| `internal/complete/complete_test.go::TestGenerate_CustomWorkflowShortcutRecentFirst` | — | Recent-first sort for workitem-backed candidates. |
| `tests/integration/workflow_create_test.go` | 68 | End-to-end: `init` → `workflow create` → `workitem create --type` → `complete re` → `go re <slug> --print`. |

### What `runCreate` does today (`create.go:54-95`)

1. `validatePathSegment("type", opts.Type)` and `("shortcut", opts.Shortcut)`.
2. Normalize the shortcut via `nav.NormalizeNavigationName`.
3. `config.LoadCampaignConfigFromCwd(ctx)` to locate the campaign root.
4. `os.MkdirAll(workflow/<type>/, 0o755)` — **single directory only**.
5. `writeOBEYIfMissing(absPath, type, title)` — writes `OBEY.md` only if absent
   (idempotent; never overwrites a user file).
6. `upsertShortcut(...)` — case-normalized; removes case-variants; returns a
   validation error on collision unless `--replace` is set; persists via
   `config.SaveJumpsConfig`.
7. `upsertConcept(...)` — case-insensitive name match; same collision semantics;
   persists via `config.SaveCampaignConfig`.
8. `invalidateNavigationCache(cmd, campaignRoot)` via `navindex.Delete`.
9. Prints a single human-formatted block:

   ```
   created workflow/<type>
     shortcut: <key> -> workflow/<type>/
     workitem type: <type>
   next: camp workitem create <slug> --type <type>
   ```

### Confirmed gaps this sequence must close

These are the implementation targets. Each is asserted against the current code,
not against a planning document.

- **No status directories.** `create.go:75` creates `workflow/<type>/` only.
- **No `list`, `show`, `shortcut add`, `doctor`, `sync` subcommands.**
  `workflow.go:16` registers only `newCreateCommand()`.
- **No `--dry-run` flag** on `create`. `runCreate` writes immediately.
- **No `--json` flag** on `create`. Output is a single human-formatted block.
- **Idempotency on rerun is not asserted in output.** Re-running with identical
  args does not error today (verified by `TestRunCreateIdempotentForSameWorkflow`
  in `create_test.go:288`), but the output is identical between first and
  second run; the user has no signal that nothing changed. This sequence must
  emit a `no changes` indicator and (for `--json`) a stable `applied: false`
  flag.

### Adjacent context the design depends on

- `internal/workitem/discover_custom_workflows.go:40` skips `dungeon` and
  `.`-prefixed directories at the `workflow/<type>/<child>` level. It walks one
  level deep and requires a `.workitem` marker. **Adding `inbox/active/ready/`
  at this level introduces directories the discoverer will see as candidate
  workitems and skip (no `.workitem`).** That is acceptable for this sequence —
  workitems still live at `workflow/<type>/<slug>/` until a later sequence
  changes placement. See §3.4 below.
- `internal/commands/workitem/create.go:37` defaults `--dir` to
  `workflow/<type>` (no status sub-dir). This sequence does **not** change that
  default; the status-dir contract is documented but discovery and placement
  changes are deferred.
- `internal/workitem/json.go:16` defines `SchemaVersion = "workitems/v1alpha5"`
  with the pattern: `schema_version`, `generated_at`, payload body. The new
  `camp workflow` JSON outputs follow the same pattern with their own schema
  family (`workflow/v1`).

---

## 2. Boundary: workitem-collection vs flow-status workflow

Two unrelated concepts share the word "workflow" in this repo. The sequence
goal and this design touch **only** the first.

| Concept | Location | What it is |
|---|---|---|
| **Workitem-collection workflow** (this design) | `workflow/<type>/` under the campaign root | A user-defined collection of workitems of one type (`research`, `feature`, `bug`, ...). Persisted in `.campaign/settings/jumps.yaml` (shortcut) and `.campaign/campaign.yaml` (concept). |
| **Flow-status workflow** (unrelated) | `internal/workflow/` (package), `.workflow.yaml` per workitem | A per-workitem state machine with phases and gates, owned by the `flow` command family (`internal/commands/flow/`). |

User-visible help text on every new subcommand must reinforce this boundary.
The phrase "workflow collection" is used throughout this doc and in user output;
"flow" or ".workflow.yaml" is never used for the workitem-collection concept.

---

## 3. Status-directory scaffold

### 3.1 Layout

`camp workflow create <type>` writes the following tree under
`workflow/<type>/`:

```
workflow/<type>/
├── OBEY.md
├── inbox/
│   └── .gitkeep
├── active/
│   └── .gitkeep
├── ready/
│   └── .gitkeep
└── dungeon/
    ├── completed/
    │   └── .gitkeep
    ├── archived/
    │   └── .gitkeep
    └── someday/
        └── .gitkeep
```

This mirrors `.campaign/intents/` layout, with the `dungeon/` leaf names
specified by the sequence goal (`completed`, `archived`, `someday`).

> **Divergence note.** Today, `.campaign/intents/dungeon/` contains
> `{killed,done,someday,archived}`. The sequence goal calls for
> `{completed,archived,someday}` for workflow collections. The design follows
> the sequence goal verbatim; aligning intents and workflows under one
> dungeon-naming scheme is out of scope for this sequence and tracked as an
> open question in §8.

### 3.2 `.gitkeep` strategy

Each status directory contains one empty `0o644` file named `.gitkeep`. Rationale:

- Git does not track empty directories; the scaffold must survive a clean
  checkout so `cd workflow/<type>/inbox` works for a new contributor with
  no workitems present.
- `.gitkeep` is the standard convention (over alternatives like `.keep` or
  `.gitignore`). It is purely a marker; no consumer reads its contents.
- The file is leading-dot, so `discover_custom_workflows.go:40`'s
  `strings.HasPrefix(name, ".")` filter already skips it.

### 3.3 Atomic-creation pattern

The scaffold is created via a `plan → apply` split (also reused by `--dry-run`
and `--json`; see §4):

1. **Plan phase** (no writes). Enumerate every path the command would create
   or modify:
   - the seven directories listed in §3.1
   - the seven `.gitkeep` files
   - `OBEY.md` (only if absent)
   - shortcut upsert in `jumps.yaml`
   - concept upsert in `campaign.yaml`
   For each, classify as `create | skip-exists | update | no-op`.
2. **Apply phase** (writes). Iterate the plan and apply.

The implementation uses `os.MkdirAll(0o755)` per directory and
`os.WriteFile(0o644)` per `.gitkeep`. Directories that already exist are
classified `skip-exists` in the plan and silently skipped at apply time
(idempotency requirement, §5). No partial-rollback semantics: a write failure
mid-apply returns the wrapped error and leaves whatever already succeeded on
disk — matching the existing `runCreate` behavior. Cleanup is the operator's
job, the same as today.

### 3.4 Interaction with `camp workitem create`

This sequence does **not** change where `camp workitem create --type <T>`
places new items. They continue to land at `workflow/<T>/<slug>/`, the
sibling of the `inbox/active/ready/dungeon/` scaffold.

The status directories are scaffolded ahead of a later sequence (planned in
the phase plan, not this sequence) that will:

- Change `workitem create` default placement to `workflow/<type>/inbox/<slug>`.
- Teach `discover_custom_workflows.go` to walk one level deeper for
  status-prefixed children.

Scaffolding the directories now lets users adopt the convention manually
(`mv workflow/research/foo workflow/research/active/foo`) and gives the
follow-up sequence a stable target.

---

## 4. New flags on `create`

### 4.1 `--dry-run`

```
camp workflow create <type> --shortcut <key> [--title <t>] [--replace] --dry-run
```

- Behavior: runs the plan phase from §3.3 and prints the plan; performs no
  writes. Exit code 0 on a clean plan, non-zero only on validation/lookup
  errors (e.g. collision without `--replace`).
- Human output format:

  ```
  plan: create workflow research
    create  workflow/research/
    create  workflow/research/OBEY.md
    create  workflow/research/inbox/
    create  workflow/research/inbox/.gitkeep
    ... (one line per planned action)
    update  .campaign/settings/jumps.yaml (shortcut re)
    update  .campaign/campaign.yaml (concept research)
  dry run: nothing written. re-run without --dry-run to apply.
  ```

- Status keywords printed: `create`, `skip-exists`, `update`, `no-op`.
- Composes with `--json` (§4.2): `--dry-run --json` emits the plan as JSON
  with `applied: false`.

### 4.2 `--json`

```
camp workflow create <type> --shortcut <key> ... --json [--dry-run]
```

- Behavior: emits exactly one JSON document to stdout. No human-readable
  text on stdout. Warnings (e.g. cache-invalidation failure) still go to
  stderr as plain text, matching `invalidateNavigationCache`'s current
  behavior.
- Default invocation (no `--json`, no `--dry-run`) output is **byte-identical
  to PR #298**. This is asserted by a snapshot test in the implementation task
  (`golden_create_output_test.go`).
- Schema (v1, flat shape per task contract):

  ```json
  {
    "schema_version": "workflow/v1",
    "generated_at": "2026-05-25T03:30:00Z",
    "type": "research",
    "title": "Research",
    "workflow_dir": "workflow/research/",
    "status_dirs": [
      "inbox/",
      "active/",
      "ready/",
      "dungeon/completed/",
      "dungeon/archived/",
      "dungeon/someday/"
    ],
    "obey_written": true,
    "shortcut": {"key": "re", "path": "workflow/research/", "replaced": false},
    "concept": {"name": "research", "path": "workflow/research/", "replaced": false},
    "replaced": [],
    "no_changes": false,
    "dry_run": false,
    "applied": true
  }
  ```

  - `obey_written` is `true` only when `OBEY.md` was actually written (false
    when pre-existing).
  - `shortcut.replaced` / `concept.replaced` are `true` when `--replace`
    overrode an existing target.
  - `replaced[]` lists shortcut keys removed under `--replace` (case-variant
    cleanup from `upsertShortcut`).
  - `no_changes` is `true` iff every action would be a no-op (re-run with
    identical args on an already-scaffolded tree).
  - `dry_run` mirrors the flag; `applied` is `!dry_run`.
- A richer `actions[]` schema (one element per planned write with `kind`,
  `target`, `object`) is deferred. The flat shape above is the v1 contract
  consumed by the rest of this sequence.

### 4.3 Mutual exclusion

- `--dry-run` may compose with `--json`.
- `--replace` is orthogonal and may compose with both.
- All flags use `cobra.Command.Flags()` (not persistent flags). The required
  `--shortcut` rule (`MarkFlagRequired`) is preserved.

---

## 5. Idempotency on rerun

Existing test `TestRunCreateIdempotentForSameWorkflow` (`create_test.go:288`)
already shows the second invocation does not error. The sequence goal
strengthens the contract:

- **Identical args** (same `<type>`, `--shortcut`, optional `--title`): exit 0
  with output `no changes for workflow <type>`. JSON form sets `applied: false`
  and every `actions[].kind` is `skip-exists` or `no-op`.
- **Same `<type>`, different shortcut/title**: today this would silently upsert
  (verified: `upsertShortcut`/`upsertConcept` blindly add the new entry).
  This design requires it to error with the existing `--replace` collision
  pattern.
- **`--replace`**: same as today — overrides the collision check; idempotency
  on the new values still applies (second `--replace` with identical args
  reports `no changes`).
- **Status-directory creation is idempotent** because the plan classifies
  existing directories as `skip-exists` (§3.3). Re-running on a partial
  scaffold (some dirs present, some missing) creates only the missing pieces.

The "no changes" string is the same in human and JSON output (`applied: false`)
so consumers can dispatch on either.

---

## 6. New subcommands

All live under `internal/commands/workflow/` next to `create.go`. Shared
helpers (plan struct, action enum, scaffold enumeration, doctor finding
codes) live in a private `internal.go` in the same package. No new packages.

The `parent` command registers them as:

```go
cmd.AddCommand(newCreateCommand())
cmd.AddCommand(newListCommand())
cmd.AddCommand(newShowCommand())
cmd.AddCommand(newShortcutCommand()) // group with `add` sub
cmd.AddCommand(newDoctorCommand())
cmd.AddCommand(newSyncCommand())
```

### 6.1 `camp workflow list`

```
camp workflow list [--json]
```

Lists every workflow collection: any directory under `workflow/` that has a
custom (non-builtin) type, or that has an upserted concept in `campaign.yaml`
with a `workflow/` path. Builtin types (`intent`, `design`, `explore`,
`festival`) are excluded — they are not user-created workflow collections.

- Resolution algorithm:
  1. Read `workflow/*` directories on disk.
  2. Load `cfg.Concepts()` and filter for paths under `workflow/` whose name
     is not in `builtinTypes` (`internal/workitem/discover_custom.go:9`).
  3. Union; sort by type name.
- For each entry: type name, shortcut (or `-` if missing), path, workitem
  count (count of `.workitem` markers walked from the type root, capped at
  some sensible ceiling like 1000 with a `+` suffix to avoid surprise IO),
  has-scaffold (`true`/`false` — does it have the §3.1 status dirs).
- Human output: aligned table.
- JSON schema (`workflow/v1`):

  ```json
  {
    "schema_version": "workflow/v1",
    "generated_at": "...",
    "workflows": [
      {
        "type": "research",
        "shortcut": "re",
        "path": "workflow/research/",
        "title": "Research",
        "workitem_count": 12,
        "workitem_count_capped": false,
        "has_scaffold": true
      }
    ]
  }
  ```

- Exit codes: 0 (always, even if empty), 1 (campaign-root lookup failure).

### 6.2 `camp workflow show <type>`

```
camp workflow show <type> [--json]
```

Detail view for one workflow collection. Resolves `<type>` via
`pathsafe.ValidateSegment` then looks up the concept and the directory.

- Human output sections: header (type, title, path), shortcut (key, source,
  description), scaffold status (per-directory present/missing), workitem
  summary (count per status dir if scaffold is present and discovery is
  status-aware, else flat count), `OBEY.md` first line.
- JSON schema extends `list`'s `workflows[]` entry shape with:
  - `obey_first_line: string`
  - `scaffold: { inbox: bool, active: bool, ready: bool, dungeon: bool, dungeon_completed: bool, dungeon_archived: bool, dungeon_someday: bool }`
  - `workitems: { total: int, by_status: { ... } | null }` — `by_status` is
    `null` until the later sequence makes discovery status-aware.
- Exit codes: 0 found, 2 unknown type (no concept, no directory), 1 IO/config
  failure.

### 6.3 `camp workflow shortcut add <type> <key>`

```
camp workflow shortcut add <type> <key> [--replace] [--json]
```

Adds a navigation shortcut for an existing workflow collection. This is the
unbundled form of the `--shortcut` flag on `create`, for the case where the
user wants a second shortcut for an existing collection or skipped
`--shortcut` somehow (today `create` requires it; future scaffolding may
relax that).

- Validation: `<type>` must exist as a directory under `workflow/` OR have a
  concept entry; otherwise error `unknown workflow type`. `<key>` is
  validated via `pathsafe.ValidateSegment`.
- Reuses `upsertShortcut`. The concept is left untouched (single concept
  per type).
- Human output: `shortcut added: <key> -> workflow/<type>/`.
- JSON: same `workflow/v1` shape with a single-action `actions` array.
- Exit codes: 0 success, 2 unknown type, 3 collision without `--replace`,
  1 IO/config failure.

### 6.4 `camp workflow doctor`

```
camp workflow doctor [--json] [--fix]
```

Reports inconsistencies in the workflow surface. Read-only by default.
`--fix` is reserved for future use; in this sequence it is parsed but
errors with `--fix is not yet implemented` (so the flag exists in the
contract without a partial implementation).

- **Finding codes** (stable strings, included in human and JSON output):

  | Code | Meaning |
  |---|---|
  | `MISSING_SHORTCUT` | A workflow type has a directory under `workflow/` but no shortcut points to it. |
  | `ORPHAN_WORKFLOW` | A shortcut in `jumps.yaml` points to `workflow/<type>/` but the directory does not exist. |
  | `MISSING_CONCEPT` | A workflow directory exists but no concept entry references it. |
  | `ORPHAN_CONCEPT` | A concept entry points to `workflow/<type>/` but the directory does not exist. |
  | `CUSTOM_TYPE_WITHOUT_WORKFLOW` | A `.workitem` marker uses a custom `type` that has no `workflow/<type>/` collection. |
  | `STALE_NAV_CACHE` | `navindex.Stat(campaignRoot)` reports a cache file with an mtime older than the newest modification under `workflow/` or `.campaign/settings/jumps.yaml`. |
  | `DUPLICATE_SHORTCUT` | After case-normalization, two shortcut keys collapse to the same value (defensive check; `upsertShortcut` should prevent this, but past hand-edits may have introduced it). |
  | `INCOMPLETE_SCAFFOLD` | A workflow type's directory exists but is missing one or more status dirs from §3.1. |

- Severity: every finding is `warning` for this release. `error` is reserved
  for future use.
- Human output: one line per finding, prefixed with the code and the
  affected path:

  ```
  doctor: 3 findings
    MISSING_SHORTCUT       workflow/research/
    ORPHAN_CONCEPT         workflow/old-stuff/ (concept "old-stuff")
    INCOMPLETE_SCAFFOLD    workflow/feature/ (missing: active/, dungeon/completed/)
  ```

- JSON schema:

  ```json
  {
    "schema_version": "workflow/v1",
    "generated_at": "...",
    "findings": [
      {
        "code": "MISSING_SHORTCUT",
        "severity": "warning",
        "type": "research",
        "path": "workflow/research/",
        "detail": "no shortcut points to workflow/research/"
      }
    ]
  }
  ```

- Exit codes: 0 if no findings, 1 if any finding present (consistent with
  `git fsck`, `eslint`, etc.; gives CI an easy signal).

### 6.5 `camp workflow sync`

```
camp workflow sync [--json] [--dry-run]
```

Reconciles `jumps.yaml`, `campaign.yaml`, and `workflow/<type>/` directories
to the post-#298 contract — same `plan → apply` pattern as `create`. The
contract:

- For every directory under `workflow/` that has a custom (non-builtin) type
  and has at least one `.workitem` marker walked from its tree, ensure a
  shortcut and concept exist. Shortcut key defaults to the first two letters
  of the type name; collisions fall back to three letters; on further
  collision, leave it unset and emit a `MISSING_SHORTCUT` doctor finding.
- For every concept whose path points to `workflow/<type>/`, ensure the
  directory exists (create only the type root, **not** the status scaffold —
  scaffolding is `create`'s job, not `sync`'s, to keep blast radius small).
- Invalidate the nav cache once at the end.
- The default invocation does **not** add the §3.1 scaffold to existing type
  directories — that is opt-in via `--with-scaffold` (added by this command
  too):

  ```
  camp workflow sync --with-scaffold
  ```

  `--with-scaffold` runs the §3.3 scaffold plan against every existing
  workflow type that is missing one or more status dirs.

- `--dry-run` and `--json` follow the same conventions as `create` (§4).
- Exit codes: 0 success or no-op, 1 IO/config failure.

---

## 7. Idempotency-on-rerun matrix

| Scenario | Behavior | Exit |
|---|---|---|
| First `create research --shortcut re` | Apply full plan. Print plan. | 0 |
| Rerun with identical args | All `actions[].kind = skip-exists/no-op`. Print `no changes for workflow research`. | 0 |
| Rerun with different shortcut, no `--replace` | Validation error. | non-zero (validation) |
| Rerun with different shortcut and `--replace` | Replace old shortcut, leave directory scaffold untouched. | 0 |
| Rerun on a partial scaffold (some dirs missing) | Create only missing dirs. | 0 |
| `--dry-run` on a fresh repo | Print full plan, no writes. | 0 |
| `--dry-run` after first apply | Print plan with all `skip-exists`/`no-op`. | 0 |

---

## 8. Open questions (out of scope, tracked here)

1. **Dungeon naming alignment.** Intents use `dungeon/{killed,done,someday,archived}`;
   workflows will use `dungeon/{completed,archived,someday}`. Whether to
   converge them (and which set wins) is a separate decision tracked outside
   this sequence.
2. **Status-aware discovery.** `discover_custom_workflows.go` will need a
   one-level-deeper walk once `workitem create` defaults to placing items
   under `inbox/`. This sequence intentionally defers both changes; the
   `show` command's `workitems.by_status` field is `null` until then.
3. **`doctor --fix`.** Parsing the flag now reserves the surface; the
   implementation is a follow-up.
4. **Shortcut auto-derivation in `sync`.** Two-letter / three-letter fallback
   is a heuristic. If collisions become common we may want a deterministic
   slug-hash fallback instead.

---

## 9. Implementation order (informative)

Subsequent tasks in this sequence should be ordered:

1. `plan → apply` refactor of `runCreate` + `--dry-run` flag.
2. `--json` flag and snapshot-test the default-invocation output (regression
   guard).
3. Scaffold (§3) added to the plan.
4. `list`, `show`, `shortcut add` (read-mostly; share helpers via internal.go).
5. `doctor` (read-only with finding codes).
6. `sync` (mutating; reuses `plan → apply`).
7. Integration coverage in `tests/integration/workflow_create_test.go` plus
   new files per subcommand.

This order keeps every step independently shippable and keeps the default
output stable until step 2's snapshot tests are in place.
