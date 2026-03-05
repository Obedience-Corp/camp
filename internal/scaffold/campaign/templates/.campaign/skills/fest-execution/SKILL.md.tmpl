---
name: fest-execution
description: Execute active festival tasks. Use when finding the next task, marking tasks completed/blocked/reset, committing with festival traceability, advancing workflow steps, and validating sequence progress.
---

# Festival Execution

Read shared contracts first: `../references/fest-command-contracts.md`.

## Core Loop

```text
fest next -> do work -> fest task completed|blocked -> fest commit -> fest validate -> repeat
```

## Link + Navigation Contract (Critical)

Festival commands from a project directory depend on an active festival-project link.

```bash
# In festival directory: link/relink current execution project path
fest link /absolute/path/to/project
fest link --show

# Shell navigation (after `eval "$(fest shell-init zsh)"`)
fgo           # toggle festival <-> linked project
fgo project   # jump to linked project
fgo fest      # jump back to linked festival
```

If the execution project path changes (moved repo, new worktree, new checkout), relink before continuing work.

## Task State

```bash
fest task completed
fest task blocked --reason "..."
fest task reset
```

Do not mix command families:

- `fest task` mutates task status.
- `fest workflow` advances phase-level workflow/gate steps.

## Visibility and Dependencies

```bash
fest show --inprogress --watch
fest show --roadmap
fest deps
```

## Workflow Steps (Phase-Level)

```bash
fest workflow status
fest workflow advance
fest workflow skip --reason "..."
```

## Validation

```bash
fest validate
fest validate <festival-path>
```

## Common Mistakes

- Using `fest task complete` / `fest task block` (wrong forms).
- Confusing `fest workflow` commands with task-status commands.
- Continuing work from a new project path without rerunning `fest link`.
- Skipping `fest next` and manually selecting tasks out of dependency order.
