# camp workitem commit — Staging Plan Design

Status: design contract for CW0003 sequence 04, task 02 implementation.
Mirrors the existing quest-commit precedent in `internal/git/commit/quest.go`.

## 1 Goal

Add `camp workitem commit` as a scoped commit command. It stages only files that
belong to the resolved workitem context and never silently widens to the whole
campaign root. The same plan flows through a new `commit.Workitem` helper that
mirrors `commit.Quest` so the campaign-tag composition, file-scoping, and result
shape are shared with every other commit path.

The design is built on top of the v1alpha6 workitem schema and the
`workitem/resolver` pipeline that sequence 03 already shipped. The resolver
returns the workitem, its quest id, and its source. The commit command turns
that resolution into a staging plan, prints it to stderr, and executes it
through `commit.Workitem`.

## 2 Precedent

We deliberately reuse, not parallel-implement, the quest path.

- `internal/git/commit/quest.go` — defines `QuestAction`, `QuestOptions`, and
  the thin `Quest()` constructor that forwards into `doCommit`.
- `internal/git/commit/commit.go` — owns `Options`, `doCommit`, and
  `stageAndCommit`. The `Options.Files` / `Options.PreStaged` /
  `Options.SelectiveOnly` triple already provides the exact selective-commit
  semantics we need: `git add <files>` followed by `git commit --only
  <files>`, with `ErrNoChanges` on empty scope rather than silent
  `CommitAll` fallback.
- `internal/git/commit/intent.go`, `internal/git/commit/project.go` — show the
  established 1-file pattern for adding a new commit family.

The workitem family follows the same shape. The only new code in the commit
package is a thin `workitem.go` mirror; all real machinery stays in `doCommit`.

## 3 commit.Workitem shape

```go
// internal/git/commit/workitem.go (new file in task 02)

package commit

import "context"

// WorkitemAction identifies the high-level operation captured by the commit.
// The generic case is WorkitemScope — a user-initiated scoped commit via
// `camp workitem commit`. WorkitemEdit is reserved for in-process flows that
// edit workitem metadata (rename, retype, retitle) and want a more specific
// action tag.
type WorkitemAction string

const (
    WorkitemEdit  WorkitemAction = "WorkitemEdit"
    WorkitemScope WorkitemAction = "WorkitemScope"
)

// WorkitemOptions configures a workitem commit. The embedded Options carries
// the campaign root, campaign id, selective-commit scope, and the QuestID /
// WorkitemRef fields that doCommit hands to FormatContextTagsFull.
type WorkitemOptions struct {
    Options
    Action      WorkitemAction
    WorkitemID  string // stable id, e.g. "design-timeline-2026-05-24"
    WorkitemRef string // WI-<6 hex>, copied into Options.WorkitemRef
    QuestID     string // optional; copied into Options.QuestID
    Title       string // workitem title used as the commit subject
    Detail      string // optional body text
}

// Workitem stages workitem-scoped changes and commits them with a campaign tag
// that carries the workitem ref (and quest id, when set).
func Workitem(ctx context.Context, opts WorkitemOptions) Result {
    opts.Options.QuestID = opts.QuestID
    opts.Options.WorkitemRef = opts.WorkitemRef
    return doCommit(ctx, opts.Options, string(opts.Action), opts.Title, opts.Detail)
}
```

This file is the entire delta to the commit package. `stageAndCommit` already
honors `Options.Files` and `Options.SelectiveOnly`; we pass the computed
staging plan as `Files` and set `SelectiveOnly = true` so empty plans surface
`(no changes to commit)` instead of widening.

## 4 Default staging matrix

The resolver returns a `*resolver.Resolution` whose `Source` tells us which
matrix row applies. The staging planner translates that resolution into the
file list passed to `commit.WorkitemOptions.Files`.

| Resolved context | Default staged paths |
|---|---|
| **Workitem directory under campaign root** (cwd inside `workflow/<type>/<slug>/`) | `<workitem-dir>/**` plus `.campaign/workitems/links.yaml` if dirty |
| **Campaign root** (cwd at root, resolver returned a workitem via `current` or explicit `--workitem`) | `<workitem-dir>/**` plus `.campaign/workitems/links.yaml` if dirty. **Never `git add .`.** |
| **Linked project repo** (cwd under a `projects/<name>/` submodule that resolves to a workitem link) | All changed files inside that project repo. The submodule is its own git repo, so "all changed files" means `git status --porcelain` inside the submodule, excluding gitignored entries. |
| **Project submodule pointer at campaign root** (changes to the submodule SHA in the parent repo) | **Off by default.** Requires `--include-submodule-pointer` or a follow-up `camp refs-sync`. |
| **Festival-linked workitem** (resolver source = `festival`) | Festival-scoped paths under `festivals/<status>/<festival>/` that touch the workitem, plus `.campaign/workitems/links.yaml` if dirty. |
| **No workitem context** | Error with a hint. Do not fall back to staging anything. |

### 4.1 Why links.yaml is part of the default plan

`camp workitem commit` is the moment a user records that a chunk of work
"belongs to" a workitem. If the link registry just changed (e.g. they ran
`camp workitem link …` and then `camp workitem commit`), staging
`.campaign/workitems/links.yaml` alongside the changes keeps the link and
the work it scoped together in a single commit. The planner checks
`git status` for the registry path; if clean, it is omitted, so we do not
churn the registry on every commit.

### 4.2 Why submodule pointer is off by default

Auto-staging the submodule SHA in the parent campaign repo is the single
biggest footgun in the existing campaign tooling. It produces "pointer
thrash" commits that move project SHAs forward as a side effect of unrelated
work in the campaign root. The workitem-commit path opts out by default and
defers pointer movement to either the explicit `--include-submodule-pointer`
flag or the existing `camp refs-sync` workflow.

## 5 Flag precedence

The default matrix can be modified with four flags. Precedence runs strictly
in this order — each step takes the prior step's plan as input:

1. **`--staged`** — short-circuits the matrix. The staging plan is exactly
   what is already in the git index. The planner skips resolution-driven
   staging and passes `Options.PreStaged` (the index contents) so the
   campaign tag still carries the workitem ref but no `git add` runs.
2. **`--project <name>`** — overrides the resolver. The planner treats the
   command as if invoked from inside that project repo (matrix row 3).
   Combined with `--workitem` for an explicit override at both axes.
3. **`--include <path>`** (repeatable) — adds the path to the plan after the
   matrix is computed. Paths must be under the campaign root or under the
   resolved project repo; out-of-scope paths error.
4. **`--exclude <path>`** (repeatable) — removes the path from the plan;
   applied last so the user can prune even matrix-default paths.

`--workitem <selector>` (already wired in `camp commit` via sequence 03) is
not a precedence step; it changes what the resolver returns, and the matrix
runs on that result.

## 6 Staging plan output

Before invoking `commit.Workitem`, the command prints the plan to **stderr**
in a fixed, parseable format. Stderr (not stdout) keeps the plan visible
during interactive use without polluting pipes that consume `--json`.

```text
workitem: <id> (ref: WI-<6 hex>)
context:  <one-line context summary>
staging:
  A  <relative path>
  M  <relative path>
skipped:
  <relative path> (<reason>)
tag:    [OBEY-CAMPAIGN-<8hex>[-qst_<…>]-WI-WI-<6hex>]
```

`A` / `M` / `D` mirror `git status --porcelain` status letters. Skipped
entries include `(gitignored)`, `(out of scope)`, `(submodule pointer; use
--include-submodule-pointer)`, and `(--exclude)`. Reasons are stable strings
so the integration tests can grep for them.

`--json` swaps the human plan for a structured `staging_plan` object
containing the same data, plus `command`, `workitem`, `tag` fields. The
human plan is suppressed when `--json` is set; errors still print to stderr.

## 7 No-context error contract

If the resolver returns no workitem and no `--workitem` selector was given,
the command errors with:

```text
Error: no workitem context resolved from cwd

Try one of:
  --workitem <selector>            explicit workitem to scope this commit
  cd workflow/<type>/<slug> && ...  run from inside a workitem directory
  camp workitem current <selector>  set a session-wide default
```

Crucially: the command does **not** fall back to staging the whole repo. The
contract is that `camp workitem commit` always carries a `WI-` tag; if we
cannot derive one, we do not commit.

## 8 Pointer thrash policy

The default plan never touches the parent-repo submodule pointer. Three
behaviors keep that promise:

1. The matrix row for "linked project repo" stages files **inside** the
   submodule's own git repo. The parent campaign repo is not touched.
2. The matrix row for "project submodule pointer at campaign root" is
   intentionally off by default. The user has to either pass
   `--include-submodule-pointer` or run `camp refs-sync` afterward.
3. When `--include-submodule-pointer` is passed, the planner stages the
   relative submodule path in the parent repo plus the file changes in the
   submodule, and the commit message carries the workitem ref so the
   pointer move is attributable.

## 9 commit.Workitem call shape

The `camp workitem commit` command builds `commit.WorkitemOptions` like:

```go
opts := commit.WorkitemOptions{
    Options: commit.Options{
        CampaignRoot:  campRoot,
        CampaignID:    cfg.ID,
        Files:         plan.Files,
        PreStaged:     plan.PreStaged,
        SelectiveOnly: true,
    },
    Action:      commit.WorkitemScope,
    WorkitemID:  wi.StableID,
    WorkitemRef: ref,
    QuestID:     resolution.QuestID,
    Title:       subject, // -m message, or workitem title fallback
    Detail:      body,
}
res := commit.Workitem(ctx, opts)
```

`SelectiveOnly: true` is the load-bearing flag. It guarantees that when
`plan.Files` is empty (e.g. user excluded everything), the commit returns
`(no changes to commit)` instead of falling through to `CommitAll`.

## 10 Sample plans

One sample per matrix row lives under
`internal/commands/workitem/testdata/plans/`:

- `workitem_dir.txt` — cwd inside a workitem directory
- `campaign_root.txt` — cwd at campaign root, workitem resolved via `current`
- `linked_project.txt` — cwd in a linked project submodule
- `submodule_pointer_explicit.txt` — `--include-submodule-pointer`
- `festival_scope.txt` — resolver source = `festival`
- `no_context_error.txt` — no workitem, no override; shows the error text

These fixtures are consumed by the task-04 integration tests; task 02 uses
them as the contract while implementing the planner.

## 11 Out of scope for sequence 04

- Cross-PR work to wire `fest commit` into the same matrix. Sequence 03
  shipped the tag composer; the fest-side bump waits on the next camp tag.
- `camp workitem commits` query command (task 03 of this sequence) is a
  separate concern. It consumes the tag format defined here but does not
  share the staging planner.
- Interactive selection of files. The plan is computed and either accepted
  or aborted; a future TUI can layer on top.
