---
name: fest-planning
description: Plan and scaffold festivals. Use when creating festival/phase/sequence/task structure, enforcing naming rules, linking festivals to projects, and promoting lifecycle states.
---

# Festival Planning

## The Loop Is The Planning Process

Create the festival, then drive `fest next`. It asks for one planning decision
at a time and writes the structure as you answer.

```bash
fest create festival --type standard --name <name>
fest next                     # answer what it asks
# ...repeat until the plan is complete...
fest validate
```

Do **not** scaffold every phase and sequence first and fill the `[REPLACE]`
markers afterward. Scaffolding a large phase in one step writes hundreds of
unfilled markers, drops `fest validate` from 100 to 0 with no partial-credit
state, and creates pressure to fill them with plausible filler just to restore
the score. The result is a green festival full of worthless tasks.

The `fest create phase` / `fest create sequence` / `fest create task` commands
below are for targeted additions to a plan that already exists, not for building
one from scratch.

## Start Here

```bash
fest understand planning
fest understand methodology
fest understand structure
fest understand tasks
fest understand rules
fest types festival
```

Other `fest understand` topics available when you need deeper context:
`checklist`, `workflow`, `context`, `templates`, `gates`, `nodeids`,
`plugins`, `extensions`, `resources`. Run `fest understand` alone for
the current topic list.

`fest types` is low-frequency but high-value: use it at festival setup time or whenever type choice is unclear.

## Naming Rules (Automation-Critical)

- Phase dir: `NNN_UPPER_CASE/` (example `001_IMPLEMENT/`)
- Sequence dir: `NN_lower_snake_case/` (example `01_auth_module/`)
- Task file: `NN_lower_snake_case.md` (example `01_create_handler.md`)

Every sequence should include `SEQUENCE_GOAL.md`.

## Festival Types (Choose Before Scaffolding)

Use `fest types festival` / `fest types festival show <type>` as source of truth for your workspace.

- `standard`: balanced default, use when work needs ingest + planning before implementation.
- `implementation`: execution-only, use when specs/plan already exist and coding can start directly.
- `research`: investigation-heavy, use when decisions depend on comparative analysis and synthesis.
- `ritual`: recurring/custom pattern, use for repeatable non-standard workflows.

Create with explicit type:

```bash
fest create festival --type standard --name <name>
fest create festival --type implementation --name <name>
fest create festival --type research --name <name>
fest create festival --type ritual --name <name> --dest ritual
```

## Phase Types (Choose by Work Shape)

- `planning`: workflow phase for architecture/decision-making (`WORKFLOW.md`, no numbered sequences).
- `research`: workflow phase for investigation and findings (`WORKFLOW.md`, sources/findings).
- `ingest`: workflow phase for normalizing incoming context/specs (`WORKFLOW.md`, input/output specs).
- `implementation`: sequence-driven coding phase (numbered sequences/tasks, quality gates).
- `review`: freeform acceptance/signoff phase (goal-driven, not task-heavy).
- `non_coding_action`: freeform operational actions (coordination, rollout, non-code tasks).

Create with explicit phase type:

```bash
fest create phase --name "001_IMPLEMENT" --type implementation
fest create phase --name "002_RESEARCH" --type research
```

## Create / Promote

```bash
fest create festival

# Targeted additions to an existing plan, not a way to build one
fest create phase
fest create sequence
fest create task

fest promote
fest validate
```

## Task Quality (Not Checked By validate)

`fest validate` scores structure, not substance. A festival of one-line tasks
validates at 100. Task quality is on you.

A task document is written for an agent that has none of the planning
conversation's context. Tutorial-grade means:

- It names the files it touches.
- It states expected behavior, including error paths, not just the happy path.
- It cites real `file:line` anchors you have actually opened. Never write an
  anchor because it sounds plausible; open the file and verify it.
- It says how to tell the task is done, in terms someone else could check.

Plan the full scope. Do not trim work out of a plan to make it look achievable,
and do not defer the hard parts to an invented later phase. Scaling down is the
user's call, made against a complete plan.

## Link + FGO Navigation (Required for Project-Context Execution)

```bash
# In festival directory
fest link [path]
fest link --show
fest links
fest unlink

# Shell helper after `eval "$(fest shell-init zsh)"`
fgo
fgo project
fgo fest
```

If the working project directory changes (new worktree, moved repo, different checkout), rerun `fest link` so `fgo`, `fest next`, and `fest commit` resolve correctly from the project path.

## Type and Scaffolding

```bash
fest types list
fest types show <type-name>
```

Advanced generation path (not the default planning entrypoint):

```bash
fest scaffold from-plan --plan STRUCTURE.md --name my-festival
```

## Common Mistakes

- Scaffolding a whole phase of sequences up front, then filling `[REPLACE]`
  markers. Drive `fest next` instead.
- Writing thin task documents because `fest validate` still scores 100.
- Using `fest link --project ...` (invalid).
- Assuming an old link still works after changing project directory location.
- Picking `implementation` type when requirements are still unclear (should be `standard` or `research` first).
- Using workflow phase types when you need numbered implementation task execution.
- Using noncompliant naming patterns (`P1-...`, `S1-...`, `T1-...`).
- Treating `fest scaffold` base command as if it performs generation by itself.
