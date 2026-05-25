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

1. `--staged` — short-circuits; skips the matrix entirely (see Known issues).
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
| `festival` | resolver source is `SourceFestival` | Changed files under the festival scope path plus `.campaign/workitems/links.yaml` when dirty |
| `staged-only` | `--staged` flag is set | Whatever is already in the git index of the campaign root |

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
| `--include` | Additional path to stage (repeatable; literal, relative to repo root). |
| `--exclude` | Path to remove from the staging plan (repeatable; exact match). |
| `--staged` | Commit whatever is already in the git index. |
| `--include-submodule-pointer` | Include dirty project submodule pointers in the plan. |
| `--dry-run` | Print the staging plan and exit without committing. |
| `--json` | Emit staging plan and commit result as JSON on stdout. |

`--include` and `--exclude` accept literal paths only. Glob patterns are not
expanded; a pattern that does not match any real path either passes through
silently to `git add` (include) or has no effect (exclude). See Known issues.

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
tag:    [OBEY-CAMPAIGN-<8hex>[-qst_<...>][-WI-WI-<6hex>]]
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

The command never falls back to staging the whole repo. A `WI-` tag in the
commit is mandatory; if one cannot be derived, the commit does not happen.

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

See Known issues for the unanchored-regex limitation in `ParseTag`.

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
.       a1b2c3d4  2026-05-25  [OBEY-CAMPAIGN-abc-WI-WI-def123] feat: ...
projects/camp  e5f6a7b8  2026-05-24  [OBEY-CAMPAIGN-abc-WI-WI-def123] fix: ...
```

Per-repo errors are reported on stderr as a summary warning. Under `--json`,
they appear in the `errors` array.

---

## Commit tag grammar

Every commit produced by `camp workitem commit` (and `camp commit` / `camp p
commit` when run in a workitem context) carries a tag with this structure:

```
[OBEY-CAMPAIGN-<campaign-id>[-<quest-id>][-FE-<festival-ref>][-WI-WI-<workitem-ref>]]
```

Segment rules:

- All four components appear in fixed order inside the bracket. Absent
  components are omitted entirely; their separators do not appear.
- `<campaign-id>` is the first 8 hex characters of the campaign UUID.
- `<quest-id>` matches `qst_<digits>_<alphanum>` when a quest is active.
- `<festival-ref>` is the festival slug when the commit is scoped to a
  festival workitem.
- `<workitem-ref>` is the canonical `WI-<6 lowercase hex>` ref. Because the
  ref already starts with `WI-`, the segment marker adds a second `WI-`,
  producing the `WI-WI-` double prefix. This is intentional. The segment
  marker and the ref are distinct: flattening to a single prefix breaks the
  parser's segment-boundary detection (see `indexOfNextPrefix` in
  `internal/git/campaign_tag.go`).

Concrete examples:

```
[OBEY-CAMPAIGN-2736169c] feat: no workitem
[OBEY-CAMPAIGN-2736169c-WI-WI-861089] fix: workitem only
[OBEY-CAMPAIGN-2736169c-qst_1_alpha-WI-WI-861089] fix: with quest
[OBEY-CAMPAIGN-2736169c-FE-CW0003-WI-WI-861089] feat: festival + workitem
```

The full composer is `FormatContextTagsFull` in
`internal/git/campaign_tag.go`. The public re-export is
`commitkit.FormatContextTagsFull` in `pkg/commitkit/`.

### ParseTag

`ParseTag(subject string) TagComponents` reverses the grammar. It returns a
zero-valued `TagComponents` when no tag is present in the subject. The four
fields of `TagComponents` are `CampaignID`, `QuestID`, `FestRef`, and
`WorkitemRef` (the last includes its `WI-` prefix, e.g. `"WI-861089"`).

The parse contract is designed for subjects produced by `FormatContextTagsFull`.
For known parser bugs, see Known issues below.

---

## JSON output schemas

### `camp workitem commit --json`

Emitted on stdout. Pretty-printed with two-space indent.

| Field | Type | Notes |
|---|---|---|
| `workitem` | string | Stable workitem ID |
| `workitem_ref` | string | `WI-<6 hex>`, omitted when empty |
| `quest_id` | string | omitted when empty |
| `tag` | string | Full campaign tag as it appears in the commit subject |
| `context` | string | Matrix row label (e.g. `"linked project"`) |
| `context_note` | string | Detail within the row (e.g. `"project camp"`), omitted when empty |
| `repo_root` | string | Absolute path of the git repo the commit targeted |
| `stage` | []string | Paths passed to `git add` |
| `pre_staged` | []string | Paths already in the index (from `--staged`), omitted when empty |
| `skip` | []object | Each entry has `path` and `reason` strings, omitted when empty |
| `sha` | string | Commit SHA after a successful commit; empty on `--dry-run` or no-changes |

Note: `schema_version` is absent from the current payload. See Known issues
(`CW0003-commit-10`).

### `camp workitem commits --json`

Emitted on stdout. Pretty-printed with two-space indent.

| Field | Type | Notes |
|---|---|---|
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

Note: `schema_version` is absent from the current payload. See Known issues
(`CW0003-commit-10`).

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

## Known issues

The following findings are open at the reviewed commit (`642504f`) and have
not yet been merged as fixes. They are cited by ID for tracking.

**`CW0003-commit-02`** — `runCommit` returns the raw error from
`commit.Workitem` without wrapping it with the `"commit workitem"` operation
context. Additionally, `lastCommitSHA` errors are silently swallowed
(`sha, _ := ...`); when the SHA lookup fails after a successful commit, the
JSON payload emits `"sha": ""` with no indication of why.

**`CW0003-commit-03`** — `cwdSubGitRepo` does not call
`filepath.EvalSymlinks` on either the current working directory or the
campaign root before computing `filepath.Rel`. On macOS, campaign roots under
symlinked paths (e.g. `/var/folders/...` resolving to `/private/var/...`)
cause the prefix comparison to fail, so `cwdSubGitRepo` returns false and
the plan routes to the campaign root instead of the project submodule the
user is actually inside. The cwd-first routing promise from the staging
matrix silently breaks for affected users.

**`CW0003-commit-10`** — Both `--json` payloads (`commit` and `commits`) are
missing a `schema_version` field. Both commands carry `"agent_allowed":
"true"` in their Cobra annotations, meaning agent tooling is expected to
depend on these payloads. Any future field rename is a silent break with no
version signal for consumers.

**`CW0003-commit-14`** — `--staged` always uses `campaignRoot` as the staging
repo root and reads from the campaign repo's index. If `camp workitem commit
--staged` is run from inside `projects/foo/`, the submodule's index is
ignored. The cwd-first detection (`cwdSubGitRepo`) is bypassed in the
`--staged` branch.

**`CW0003-format-02`** — `validateMetadata` does not validate the shape of
the `ref` or `quest_id` fields on load. A hand-edited `.workitem` marker with
a malformed `ref` value propagates through the staging plan and into the
commit tag without rejection.

**`CW0003-format-03`** — `tagShellRegex` is unanchored
(`\[OBEY-CAMPAIGN-([^\]]+)\]` with no `^`). `ParseTag` matches a campaign
tag appearing anywhere in the subject string, not only at position 0. A
revert subject like `Revert "[OBEY-CAMPAIGN-abc] feat: X"` or a commit body
that contains a sample tag will produce false-positive parse results. The
`commits` command's candidate filter (`ParseTag(subject).WorkitemRef != ref`)
inherits this behavior.

**`CW0003-format-04`** — `ParseTag` silently merges junk segments into
adjacent fields when a tag contains unknown segments, duplicate segments, or
content between known prefixes. Examples: a second `FE-` segment overwrites
the first; an unknown leading segment causes the parser to abandon the
remainder of the inner string, silently dropping a valid `WI-` segment that
follows. Downstream consumers receive plausible-looking but wrong
`TagComponents` with no indication that parsing degraded.

**`CW0003-format-13`** — (Design note, not yet a named finding in the review
index.) The `FormatContextTagsFull` composer silently omits the `WI-` segment
when `workitemRef` is empty. Commits produced by `camp commit` or `camp p
commit` outside a workitem context carry no `WI-` segment. This is correct
behavior, but callers that pass a workitem ref derived from v1alpha5 metadata
(which has no `ref` field) will silently produce a tag with no workitem
attribution. Run `camp workitem doctor --fix` to backfill refs before relying
on tag-based history queries.
