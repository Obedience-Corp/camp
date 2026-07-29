# Workitem Link Registry Reference

The workitem link registry connects a workitem to one or more scopes in the campaign, such as a project directory, festival, or worktree. Once a link exists, commit wrappers (`camp workitem commit`, `fest commit`, `camp p commit`) can automatically tag commits with the workitem reference without requiring the `--workitem` flag on every invocation.

Without a link, a workitem is passive state: it exists in `.campaign/workitems/` and tracks work, but it has no connection to the places where that work happens. A link is what makes the registry actionable.

**Scenario.** You are working on a design workitem at `workflow/design/api-refactor` and want every commit you make under `projects/myrepo` to carry `WI-api-refactor-2026-05-20` in its message automatically. Run:

```
camp workitem link api-refactor-2026-05-20 --project myrepo
```

From that point forward, any commit made from within `projects/myrepo` resolves to that workitem through the link registry, and the commit wrapper appends the `WI-<ref>` segment.

---

## Link ID Format

Every link is assigned an ID at creation time:

```
lnk_<yyyymmdd>_<6 lowercase hex>
```

Example IDs: `lnk_20260524_ab12cd`, `lnk_20260524_ef3401`.

The date component is the UTC date the link was created. The six-character suffix is sourced from `crypto/rand`. The full ID is always exactly 22 characters and matches `^lnk_[0-9]{8}_[0-9a-f]{6}$`.

IDs are immutable once created. Link creation retries ID generation up to 32
times if the random suffix collides with an existing entry.

---

## Persistence

### `links.yaml`

The durable link registry lives at:

```
<campaign-root>/.campaign/workitems/links.yaml
```

This file should be committed to the campaign repository. It records all workitem-to-scope relationships and is the shared source of truth for all users of the campaign.

Schema version: `workitem-links/v1alpha1`. The loader rejects unknown versions
with a validation error. Field-level schema rules are enforced by the
`internal/workitem/links` types and validators and summarized in this document.

The file is created on first `camp workitem link` invocation. A missing file is the valid zero state: all commands treat it as "no links."

Writes are atomic (write-temp-plus-rename). Mutating commands hold
`links.yaml.lock` around the full load/mutate/save transaction, so concurrent
`camp workitem link` and `camp workitem unlink` invocations do not silently
overwrite each other's registry updates.

### `current.yaml`

The locally selected active workitem lives at:

```
<campaign-root>/.campaign/workitems/current.yaml
```

This file is per-machine state. It should not be committed to git. `camp init`
and `camp init --repair` keep the campaign `.gitignore` configured with:

```
workitems/current.yaml
```

`current.yaml` contains a single workitem selection. Setting a new current overwrites the file atomically. `--clear` removes the file.

---

## Scope Kinds

A link's `scope` specifies what area of the campaign the workitem is attached to.

| Kind | Path resolves to | Example path |
|---|---|---|
| `project` | A project or submodule under `projects/` | `projects/myrepo` |
| `repo` | A git repository root not registered as a project | `vendor/external` |
| `campaign_path` | Any campaign-relative path (catch-all) | `workflow/design/spike` |
| `festival` | A festival path under `festivals/` | `festivals/active/myrepo-MR0001` |
| `worktree` | A project worktree under `projects/worktrees/` | `projects/worktrees/myrepo@feat-x` |

Paths are campaign-relative, forward-slash-normalized, and validated to be contained within the campaign root.

---

## Roles

| Role | Meaning |
|---|---|
| `primary` | Active workitem for the scope. Commit wrappers pick this up automatically. At most one primary per `(scope.kind, scope.path)`. |
| `related` | Context only. Shown in `camp workitem links`. Not used for commit routing. |
| `blocked_by` | Metadata only. May influence future doctor checks. |
| `supersedes` | Metadata only. Lets a newer workitem claim an older one's history. |

To change a link's role, remove and re-add it. There is no in-place role transition.

---

## Resolver Tiers

When a commit wrapper or `camp workitem resolve` needs to determine the active workitem, it consults these tiers in order. The first match wins.

1. Explicit `--workitem <selector>` flag.
2. Nearest ancestor directory containing a `.workitem` marker file.
3. Primary link in `links.yaml` whose `scope.path` is the longest prefix of the current working directory (campaign-relative).
4. Primary link whose `scope.kind` is `festival` and whose path matches the supplied festival ID.
5. `current.yaml` workitem selection.
6. No workitem context. Commit wrappers proceed without a `WI-` tag.

Tier 3 uses longest-prefix matching: a link on `projects/myrepo/internal/api` wins over one on `projects/myrepo` when both exist and `cwd` is under the deeper path.

Resolution is read-only. It never mutates `current.yaml`.

The generic commit wrappers (`camp commit`, `camp project commit`, and
`camp worktrees commit`) and intent/note auto-commits intentionally stop before
the `current.yaml` tier. This prevents a stale per-machine selection from
adding an unrelated `WI-` tag. Use `camp workitem commit` when you want the
explicit current-workitem fallback.

If a primary path link, festival link, or `current.yaml` selection points to a
workitem that no longer exists, the resolver records that tier as an error in
the trace and continues to the next tier where one exists. Operational errors
that prevent resolution for reasons other than a missing workitem still fail
the command.

---

## Commands

### `camp workitem link`

Attach a workitem to a scope.

```
camp workitem link <selector> [flags]
```

Scope is specified via exactly one of:

| Flag | Resolves to |
|---|---|
| `--project <name>` | `projects/<name>` |
| `--festival <id>` | Festival path under `festivals/` |
| `--worktree <path>` | Path under `projects/worktrees/` |
| `--cwd` | Current working directory (campaign-relative) |

Note: `--project` takes a project name, not an absolute path. `--project myrepo` resolves to `projects/myrepo`.

Additional flags:

- `--role primary|related|blocked_by|supersedes` (default `primary`)
- `--replace` overrides an existing primary link on the same scope
- `--allow-missing` skips existence checks on the workitem and scope target (useful for migrations)
- `--json` emits structured output

On success, the command prints the new `lnk_` ID and the resolved scope.

See [cli-reference/camp\_workitem\_link.md](cli-reference/camp_workitem_link.md).

### `camp workitem unlink`

Remove one or more links.

```
camp workitem unlink [selector] [flags]
```

Remove by ID:

```
camp workitem unlink --id lnk_20260524_ab12cd
```

Remove by workitem and scope:

```
camp workitem unlink my-workitem-id --project myrepo
```

Use `--all` when multiple links match the selector and you want to remove all of them. Without `--all`, an ambiguous match returns an error.

See [cli-reference/camp\_workitem\_unlink.md](cli-reference/camp_workitem_unlink.md).

### `camp workitem links`

List links, optionally filtered by workitem selector.

```
camp workitem links [selector] [flags]
```

Output is sorted by `(scope.kind, scope.path, created_at, id)`. An empty registry prints "no links" and exits 0.

`--json` emits the full link list as a structured envelope.

See [cli-reference/camp\_workitem\_links.md](cli-reference/camp_workitem_links.md).

### `camp workitem current`

Get, set, or clear the local current workitem.

```
camp workitem current [selector] [flags]
```

With no arguments, prints the currently selected workitem (or "none" if `current.yaml` is absent).

With a selector, writes the resolved workitem to `current.yaml`:

```
camp workitem current my-workitem-id
```

`--clear` removes `current.yaml` entirely.

`current.yaml` is per-machine. See the gitignore note under [Persistence](#persistence).

See [cli-reference/camp\_workitem\_current.md](cli-reference/camp_workitem_current.md).

### `camp workitem resolve`

Print the workitem the current context resolves to, without mutating any state.

```
camp workitem resolve [flags]
```

`--explain` prints the tier-by-tier trace showing which tiers matched, missed, or were skipped. Useful for debugging unexpected resolution.

`--workitem <selector>` tests explicit resolution. `--festival <id>` supplies a festival ID for tier 4.

`--json` emits the `Resolution` struct including `source`, `reason`, and the full `trace` array.

See [cli-reference/camp\_workitem\_resolve.md](cli-reference/camp_workitem_resolve.md).

### `camp workitem doctor`

Report health issues in the link registry.

```
camp workitem doctor [flags]
```

Doctor checks for:

- Links whose `workitem_id` no longer resolves on disk (orphaned workitem).
- Links whose `scope.path` no longer exists on disk (orphaned scope).
- Duplicate primary links for the same scope.
- `current.yaml` pointing to a workitem that no longer exists.
- Schema violations in `links.yaml` or `current.yaml`.

`--fix` auto-removes findings tagged `auto_fixable`. This includes broken links and a stale `current.yaml`. The fix is applied in one pass before re-checking.

`--json` emits structured finding output.

When `links.yaml` cannot be parsed or validated enough to load, `doctor`
reports a registry-level finding. With `--fix`, it quarantines the broken file
as `links.yaml.broken-<timestamp>` and bootstraps an empty registry.

See [cli-reference/camp\_workitem\_doctor.md](cli-reference/camp_workitem_doctor.md).

---

## Happy Path Walkthrough

```
# 1. Create or locate a workitem.
camp workitem create --title "API refactor" --type feature

# 2. Link it to the project you are working in.
camp workitem link api-refactor-2026-05-20 --project myrepo

# 3. Confirm the link.
camp workitem links api-refactor-2026-05-20

# 4. Optionally set it as current for other contexts.
camp workitem current api-refactor-2026-05-20

# 5. Check what the resolver sees from inside the project.
cd projects/myrepo
camp workitem resolve --explain

# 6. Commits from within projects/myrepo now carry WI-<ref> automatically.
camp workitem commit -m "refactor connection pool"

# 7. When the work is done, remove the link.
camp workitem unlink --id lnk_20260524_ab12cd
```

---

## Recovery

### Corrupt `links.yaml`

If `links.yaml` is corrupt (truncated, invalid YAML, wrong version field),
commands that need the registry fail with a validation or parse error. `doctor`
reports this as a registry-level finding, and `doctor --fix` quarantines the
broken file before creating a fresh empty registry.

Recovery options:

- Prefer `camp workitem doctor --fix` when you want the CLI to quarantine the
  bad file and unblock the registry.
- If the file is in git: `git restore .campaign/workitems/links.yaml`
- If not in git or the committed version is also bad: `rm .campaign/workitems/links.yaml`. The registry returns to zero state (no links). Re-create links manually.

### Stale `current.yaml`

If `current.yaml` is corrupt, `camp workitem current` and the resolver will fail at tier 5. Remove the file:

```
rm .campaign/workitems/current.yaml
```

This returns tier 5 to "skip" state. No links are affected.

---

## Operational Notes

- Link IDs are retried up to 32 times on collision before the command fails.
- `current.yaml` is local machine state and is ignored by `camp init` and
  `camp init --repair` scaffolds.
- The canonical example registry fixture validates against the same link
  schema used by the loader and doctor checks.
