# Workitem Commit Reference

Narrative reference for `camp workitem commit`, `camp workitem commits`, the
staging matrix, and the commit tag grammar. For flag syntax and defaults, see
the auto-generated CLI reference:

- [`docs/cli-reference/camp_workitem_commit.md`](cli-reference/camp_workitem_commit.md)
- [`docs/cli-reference/camp_workitem_commits.md`](cli-reference/camp_workitem_commits.md)

---

## Why scoped workitem commits exist

`camp commit` and `camp p commit` stamp every commit with a campaign tag but
do not restrict which files they stage. Either command can accidentally widen
scope to unrelated paths.

`camp workitem commit` adds a second constraint: the staging plan is derived
from the resolved workitem context and never silently falls back to
`git add .`. The commit carries a `WI-<ref>` segment in its campaign tag so
history queries can later retrieve all commits that belong to a specific
workitem across every linked repo.

---

## Staging matrix

When `camp workitem commit` runs, `ComputePlan` selects exactly one matrix row
based on the resolved context. Flag evaluation runs in this order before the
matrix:

1. `--staged` — short-circuits; commits the current git index for the
   campaign root or the sub-git-repo containing the current working directory.
2. `--project <name>` — overrides the resolver; treats the command as if
   invoked from inside `projects/<name>/`.
3. Matrix routing by resolved source (see table below).
4. `--include` — adds paths on top of the matrix result.
5. `--exclude` — removes paths from the plan; applied last.

| Context label | When it applies | Default staged paths |
|---|---|---|
| `workitem directory` | cwd is inside `workflow/<type>/<slug>/` (ancestor match) | Changed files under `<workitem-dir>/` plus `.campaign/workitems/links.yaml` when dirty |
| `campaign root` | cwd at campaign root; workitem resolved via `current.yaml` or explicit `--workitem` | Same as above — never `git add .` |
| `linked project` | cwd is inside a sub-git-repo under the campaign tree (cwd-first detection), or resolver source is `SourceLink` | All changed files in that project's git repo |
| `festival` | resolver source is `SourceFestival` via `--festival` or cwd under a festival directory | Changed files under the festival scope path plus `.campaign/workitems/links.yaml` when dirty |
| `staged-only` | `--staged` flag is set | Whatever is already in the git index of the campaign root, or the containing sub-git-repo when cwd is inside one |

The link registry (`.campaign/workitems/links.yaml`) is auto-included in the
`workitem directory` and `festival` rows when it is dirty. This keeps a link
registration and the work it scoped in a single atomic commit.

Submodule pointer changes in the parent campaign repo are excluded by default
from every row. Pass `--include-submodule-pointer` to include them, or use
`camp refs-sync` afterward.

### cwd-first sub-git-repo detection

Before consulting the resolver source, `ComputePlan` calls `cwdSubGitRepo`
to check whether the current working directory is inside a sub-git-repo (a
nested `.git` marker under the campaign root). When detection succeeds, that
project repo is used as the staging root regardless of how the resolver found
the workitem. This is the mechanism behind "I'm in the project, commit my
workitem changes here."

---

## `camp workitem commit [selector]`

```
camp workitem commit [selector] [flags]
```

### Selector forms

The workitem to scope can be specified as:

- a slug (`timeline-redesign-2026-05`)
- a stable ID (`design-timeline-redesign-2026-05`)
- a workitem ref (`WI-abc123`)
- a festival ref (`camp-workitem-linking-and-commits-CW0003`)
- an `lnk_`-prefixed link ID

Both the positional `[selector]` and `--workitem <selector>` accept these
forms. When both are present, `--workitem` wins. When neither is provided,
the resolver falls back to cwd and `current.yaml`.

### Flags

| Flag | Description |
|---|---|
| `-m, --message` | Commit message. Required unless `--dry-run`. |
| `--workitem` | Explicit workitem selector (overrides cwd-based resolution). |
| `--project` | Force project-repo context by name (skips resolver). |
| `--festival` | Festival ID for the festival resolver tier. Usually inferred from cwd under `festivals/<stage>/<festival>/`. |
| `--include` | Additional path to stage (repeatable; literal, relative to repo root). |
| `--exclude` | Path to remove from the staging plan (repeatable; exact match). |
| `--staged` | Commit whatever is already in the git index. |
| `--include-submodule-pointer` | Include dirty project submodule pointers in the plan. |
| `--dry-run` | Print the staging plan and exit without committing. |
| `--json` | Emit staging plan and commit result as JSON on stdout. |

`--include` and `--exclude` accept literal paths only. Glob patterns are not
expanded; a pattern that does not match any real path either passes through
silently to `git add` (include) or has no effect (exclude).

### Plan output

Before committing, the plan is printed to stderr in a stable format:

```text
workitem: <stable-id> (ref: WI-<6 hex>)
context:  <matrix row label> (<context note>)
staging:
  S  <already-staged path>
  A  <path to stage>
  A  <path> (link registry auto-included)
skipped:
  <path> (out of scope)
  <path> (submodule pointer; use --include-submodule-pointer)
  <path> (--exclude)
tag:    [<campaign-name>:<8hex>[-qst_<...>][-FE-<festival-ref>][-WI-<6hex>]]
```

`S` marks files already in the index (from `--staged` mode). `A` marks files
the planner will stage. Under `--json`, this plan is suppressed on stderr and
emitted as structured JSON on stdout instead.

### Refusal mode

When no workitem can be resolved and no `--workitem` flag was given,
`camp workitem commit` exits with code 2 and prints:

```text
no workitem context resolved from cwd

Try one of:
  --workitem <selector>            explicit workitem to scope this commit
  cd workflow/<type>/<slug> && ...  run from inside a workitem directory
  camp workitem current <selector>  set a session-wide default
```

The command never falls back to staging the whole repo. For directory-backed
workitems that pre-date v1alpha6, the planner attempts to backfill a missing
`ref` before composing the tag. If backfill fails, the commit can proceed with
a warning and the tag omits the `WI-` segment; run `camp workitem doctor
--fix` before history audits to make attribution complete.

---

## `camp workitem commits [selector]`

```
camp workitem commits [selector] [flags]
```

A read-only query that retrieves all commits attributed to a workitem ref
across the campaign root and every linked project, worktree, and repo
registered in the link registry.

### Repo enumeration

`enumerateQueryRepos` always includes the campaign root. It then loads the
link registry and adds every entry whose scope kind is `project`, `repo`, or
`worktree`. Festival-scoped entries are intentionally excluded because
festival paths live under the campaign root, which is already included. The
help text's mention of "festival repo" refers to this coverage, not a
separate enumeration.

Repos are searched concurrently under a bounded worker pool capped at
`min(runtime.NumCPU(), 8)` workers. Each repo call has a per-repo timeout of
30 seconds. Repos that are not git checkouts return silently (no error). Repos
that fail the `git log` invocation are captured in the error list.

### Candidate filtering

Each repo is queried with `git log --grep="-WI-<ref>"` to narrow candidates
cheaply. Every candidate commit subject is then parsed through
`commitkit.ParseTag` to verify the `WorkitemRef` field matches exactly. This
two-pass approach avoids committing to a full log scan for every repo while
still rejecting subjects that contain the grep pattern in non-tag positions.

### Flags

| Flag | Description |
|---|---|
| `--ref` | Query by workitem ref directly (e.g. `WI-abc123`). Skips the resolver. |
| `--workitem` | Alias for the positional `<selector>`. |
| `--limit` | Maximum commits to return (default 100). |
| `--offset` | Number of commits to skip after sorting. |
| `--json` | Emit JSON instead of the default table. |

Results are sorted newest first across all repos before `--limit` and
`--offset` are applied.

### Table output

```
REPO    SHA       DATE        SUBJECT
.       a1b2c3d4  2026-05-25  [obey-campaign:2736169c-WI-def123] feat: ...
projects/camp  e5f6a7b8  2026-05-24  [obey-campaign:2736169c-WI-def123] fix: ...
```

Per-repo errors are reported on stderr as a summary warning. Under `--json`,
they appear in the `errors` array.

---

## Commit tag grammar

Every commit produced by `camp workitem commit` (and `camp commit` / `camp p
commit` when run in a workitem context) carries a tag with this structure:

```
[<campaign-name>:<campaign-id>[-<quest-id>][-FE-<festival-ref>][-<workitem-ref>]]
```

Segment rules:

- The leading token is the slugified campaign name followed by `:` and the
  short campaign id. The colon separates the name (which may itself contain
  hyphens) from the rest of the tag, which uses `-` between components.
- `<campaign-name>` is the campaign's name, lowercased and slugified (spaces
  and other separators become hyphens) via the shared `internal/slug`
  generator.
- All remaining components appear in fixed order inside the bracket. Absent
  components are omitted entirely; their separators do not appear.
- `<campaign-id>` is the first 8 hex characters of the campaign UUID.
- `<quest-id>` matches `qst_<digits>_<alphanum>` when a quest is active.
- `<festival-ref>` is the festival slug when the commit is scoped to a
  festival workitem.
- `<workitem-ref>` is the canonical `WI-<6 lowercase hex>` ref, embedded
  verbatim. It is self-identifying via its own `WI-` prefix, the same way quest
  ids lead with `qst_`. The parser also accepts the historical doubled
  `WI-WI-<6 hex>` form for commits written before this was simplified.

When the campaign name cannot be resolved or slugifies to nothing, the tag
falls back to the legacy `[OBEY-CAMPAIGN-<campaign-id>...]` form. The parser
recognizes both forms, so the entire pre-existing commit history still
resolves correctly.

Concrete examples:

```
[obey-campaign:2736169c] feat: no workitem
[obey-campaign:2736169c-WI-861089] fix: workitem only
[obey-campaign:2736169c-qst_1_alpha-WI-861089] fix: with quest
[obey-campaign:2736169c-FE-CW0003-WI-861089] feat: festival + workitem
[OBEY-CAMPAIGN-2736169c] feat: legacy fallback (name unavailable)
```

The composers are `FormatContextTagsFull` / `FormatContextTagsFullNamed` in
`pkg/commitkit/`, wrapping `internal/git/campaign_tag.go`.

### ParseTag

`ParseTag(subject string) TagComponents` reverses the grammar. It returns a
zero-valued `TagComponents` when no tag is present in the subject. The four
fields of `TagComponents` are `CampaignID`, `QuestID`, `FestRef`, and
`WorkitemRef` (the last includes its `WI-` prefix, e.g. `"WI-861089"`).

The parse contract is designed for subjects produced by `FormatContextTagsFull`.
`ParseTag` only accepts a leading tag. Embedded examples in revert subjects,
commit bodies, or later subject text are intentionally ignored. Callers that
need warning details for malformed or degraded tags can use
`ParseTagDetailed`.

---

## JSON output schemas

### `camp workitem commit --json`

Emitted on stdout. Pretty-printed with two-space indent.

| Field | Type | Notes |
|---|---|---|
| `schema_version` | string | Always `workitem-commit/v1alpha1` |
| `workitem` | string | Stable workitem ID |
| `workitem_ref` | string | `WI-<6 hex>`, omitted when empty |
| `quest_id` | string | omitted when empty |
| `festival_ref` | string | Festival ref used in the `FE-` tag segment, omitted when empty |
| `tag` | string | Full campaign tag as it appears in the commit subject |
| `context` | string | Matrix row label (e.g. `"linked project"`) |
| `context_note` | string | Detail within the row (e.g. `"project camp"`), omitted when empty |
| `repo_root` | string | Absolute path of the git repo the commit targeted |
| `stage` | []string | Paths passed to `git add` |
| `pre_staged` | []string | Paths already in the index (from `--staged`), omitted when empty |
| `skip` | []object | Each entry has `path` and `reason` strings, omitted when empty |
| `sha` | string | Commit SHA after a successful commit; omitted on `--dry-run` or no-changes |
| `warnings` | []string | Planner warnings such as legacy ref backfill notes, omitted when empty |

### `camp workitem commits --json`

Emitted on stdout. Pretty-printed with two-space indent.

| Field | Type | Notes |
|---|---|---|
| `schema_version` | string | Always `workitem-commits/v1alpha1` |
| `commits` | []CommitRecord | Sorted newest first, post-limit/offset |
| `errors` | []object | Per-repo failures; each entry has `repo` and `error` strings; omitted when empty |

Each `CommitRecord`:

| Field | Type | Notes |
|---|---|---|
| `sha` | string | Full commit SHA |
| `author` | string | Author name |
| `date` | string | RFC 3339 timestamp |
| `subject` | string | Full commit subject line |
| `repo` | string | Campaign-relative path of the repo (`"."` for campaign root) |
| `tag` | object | Parsed `TagComponents` (fields: `CampaignID`, `QuestID`, `FestRef`, `WorkitemRef`) |

---

## Failure modes

| Error | Meaning | Recovery |
|---|---|---|
| `commit message required` | `-m` was not passed and `--dry-run` is not set | Add `-m "message"` |
| `no workitem context resolved from cwd` (exit 2) | Resolver found no workitem; no `--workitem` given | Use `--workitem <selector>`, navigate into the workitem directory, or set `camp workitem current` |
| `empty staging plan` | The plan has no files to commit | Check that the workitem directory has uncommitted changes, or pass `--include <path>` |
| `no workitem context resolved; pass <selector> or --ref WI-...` | `commits` command received no selector and no workitem is resolved | Pass a selector or `--ref WI-<hex>` directly |
| `workitem has no ref; run camp workitem doctor --fix` | The resolved workitem pre-dates v1alpha6 and has no `ref` field | Run `camp workitem doctor --fix` to backfill |
| `not in a campaign directory` | Command run outside any campaign root | Navigate to the campaign root or a subdirectory |
| Per-repo query failure (table: stderr warning; JSON: `errors[]`) | `git log` in a linked repo timed out or failed | Check that the repo is accessible; re-run with `--json` for the error detail |

---

## Operational notes

- `runCommit` wraps commit execution failures with `commit workitem` context
  and prints a warning if the post-commit SHA lookup fails.
- Sub-git-repo detection canonicalizes symlinked cwd and campaign-root paths
  before choosing the repo to stage from.
- `ref` values must match `WI-<6 lowercase hex>` and `quest_id` values must
  match the supported quest-id shape when `.workitem` metadata is loaded.
- `ParseTagDetailed` returns warnings for duplicate, unknown, or malformed tag
  segments. `ParseTag` returns the parsed components only.
