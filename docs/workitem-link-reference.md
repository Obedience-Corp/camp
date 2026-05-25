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

IDs are immutable once created. The registry spec (SCHEMA.md §4) defines a retry-on-collision loop of up to 32 attempts for the rare case where a newly generated ID collides with an existing one. **Known issue `CW0003-links-01`:** the retry loop is not yet implemented. A collision currently surfaces as a hard error rather than being transparently retried. At normal usage volumes (hundreds of links per campaign) a collision is extremely unlikely, but the contract is not yet met.

---

## Persistence

### `links.yaml`

The durable link registry lives at:

```
<campaign-root>/.campaign/workitems/links.yaml
```

This file should be committed to the campaign repository. It records all workitem-to-scope relationships and is the shared source of truth for all users of the campaign.

Schema version: `workitem-links/v1alpha1`. The loader rejects unknown versions with a validation error. Full field-level schema documentation is in [`internal/workitem/links/SCHEMA.md`](../internal/workitem/links/SCHEMA.md).

The file is created on first `camp workitem link` invocation. A missing file is the valid zero state: all commands treat it as "no links."

Writes are atomic (write-temp-plus-rename). Two concurrent `camp workitem link` invocations use a file lock during the write phase, but the read-modify-write window is not locked. **Known issue `CW0003-links-06`:** a concurrent read before a locked write means last-writer-wins in multi-terminal or multi-agent scenarios. In the single-user CLI case this is unlikely to cause data loss, but the window exists.

### `current.yaml`

The locally selected active workitem lives at:

```
<campaign-root>/.campaign/workitems/current.yaml
```

This file is per-machine state. It should not be committed to git. The SCHEMA.md §1.1 specifies that `camp init` writes `.campaign/workitems/current.yaml` to the campaign `.gitignore`. **Known issue `CW0003-links-02`:** the gitignore entry is not yet written by `camp init`. Until this is resolved, `current.yaml` will appear as an untracked file in `git status` and can be accidentally staged.

**Workaround until `CW0003-links-02` lands:** add the following line to `.campaign/.gitignore` manually:

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

**Known issue `CW0003-links-03`:** when a primary link exists at tier 3 but its `workitem_id` no longer resolves on disk (workitem moved or deleted), the resolver returns an error and exits 1 instead of falling through to tier 4 or tier 5. The intended behavior is to record the tier as a miss and continue. Until this is fixed, an orphaned primary link will block commit wrappers operating from within that scope. Use `camp workitem doctor --fix` to remove the broken link, or `--workitem <id>` to override for a single operation.

**Known issue `CW0003-links-13` (proposed):** resolver fall-through behavior for broken links at tiers 4 and 5 may have similar propagation gaps. Under investigation.

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

**Known issue `CW0003-links-12` (proposed):** when `links.yaml` is corrupt enough to prevent loading, doctor may itself fail rather than reporting the corruption as a finding. Until this is addressed, see [Recovery](#recovery) below.

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

If `links.yaml` is corrupt (truncated, invalid YAML, wrong version field), all commands that load the registry will fail with a validation or parse error. This includes `doctor`.

Recovery options:

- If the file is in git: `git restore .campaign/workitems/links.yaml`
- If not in git or the committed version is also bad: `rm .campaign/workitems/links.yaml`. The registry returns to zero state (no links). Re-create links manually.

**Known issue `CW0003-links-12` (proposed):** doctor does not yet produce a structured finding for a registry that fails to load; it fails with an error instead. Manual recovery is required until that lands.

### Stale `current.yaml`

If `current.yaml` is corrupt, `camp workitem current` and the resolver will fail at tier 5. Remove the file:

```
rm .campaign/workitems/current.yaml
```

This returns tier 5 to "skip" state. No links are affected.

---

## Known Issues

| ID | Severity | Summary |
|---|---|---|
| `CW0003-links-01` | major | ID generation retry loop is missing. A collision returns an error instead of re-rolling. |
| `CW0003-links-02` | major | `current.yaml` is not added to `.campaign/.gitignore` by `camp init`. Workaround: add `workitems/current.yaml` to `.campaign/.gitignore` manually. |
| `CW0003-links-03` | major | Resolver hard-errors on a broken primary link instead of falling through to lower tiers. |
| `CW0003-links-04` | major | The canonical `testdata/example_links.yaml` fixture contains an invalid ID (`ef34gh` is not hex) and fails its own schema validation. |
| `CW0003-links-06` | minor | Read-modify-write window in `link` and `unlink` is not locked. Concurrent invocations can silently overwrite each other. |
| `CW0003-links-12` | proposed | Doctor wedges on an unloadable registry instead of reporting the corruption as a finding. |
| `CW0003-links-13` | proposed | Resolver fall-through behavior for broken links at tiers 4 and 5 may propagate the same hard-error pattern as `CW0003-links-03`. |
