---
name: fest-execution
description: Execute festival tasks - find next work, track progress, commit with traceability, advance through workflow steps, and validate completion. Use when actively working through a festival's tasks.
---

# Festival Execution

The execution loop: find next task, do the work, track progress, commit, validate, repeat.

## The Execution Loop

```
fest next → read task → do work → fest show --inprogress → fest commit → fest validate → fest next
```

### 1. Find Next Task

```bash
fest next                        # Returns the next task with full context
```

This shows:
- The task document content
- Which phase and sequence it belongs to
- Any dependencies or blockers
- The sequence goal for context

### 2. Track Progress

**Visualize progress** with `fest show`:

```bash
fest show --inprogress           # Expand only in-progress phases/sequences
fest show --inprogress --watch   # Live-updating progress view (recommended)
fest show --roadmap              # Full execution roadmap with task statuses
fest show --summary              # Aggregate summary stats
fest show --collapsed            # Collapsed tree with counters only
```

`fest show --inprogress --watch` is the most useful during execution — it continuously refreshes and shows only what you're working on, minimizing noise.

**Update task status** with `fest progress` or `fest task`:

```bash
fest progress --task <id> --in-progress    # Mark task as started
fest progress --task <id> --complete       # Mark task as done
fest task complete                         # Mark current task as complete
fest task block --reason "..."             # Block task with reason
```

### 3. Commit with Traceability

```bash
fest commit                      # Festival-aware git commit
```

`fest commit` automatically:
- References the current task in the commit message
- Adds festival metadata for progress tracking
- Enables metrics collection for execution analysis

Always use `fest commit` instead of raw `git commit` when working on festival tasks.

### 4. Advance Through Workflow

Some phases use workflow-based execution with defined steps:

```bash
fest workflow status              # Show current workflow step
fest workflow advance             # Move to next step
```

### 5. Validate

```bash
fest validate                    # Check structure and completeness
```

Run after completing each sequence to catch issues early.

## Status and Context

```bash
fest show --inprogress --watch   # Live progress view (primary tracking tool)
fest show                        # Full festival structure tree
fest status                      # Entity status details
fest context                     # Current session context (festival + project)
fest deps                        # Task dependency graph
```

## Quality Gates

Sequences often end with quality gate tasks:

1. **Testing** (`NN_testing.md`) - Run tests, verify functionality
2. **Review** (`NN_review.md`) - Code review, architecture check
3. **Iterate** (`NN_iterate.md`) - Address feedback, fix issues

These are standard tasks - execute them like any other, but they gate progression to the next sequence.

## Searching and Querying

```bash
fest search "auth"               # Search festivals by content
fest commits                     # View commits tied to festival tasks
fest parse <file>                # Parse task document into structured data
```

## Common Mistakes

- Not using `fest commit` - loses progress tracking and traceability
- Skipping `fest next` and picking tasks manually - may miss dependencies
- Not running `fest validate` after completing a sequence - issues compound
- Ignoring quality gate tasks (testing, review, iterate) - they exist for a reason
- Working on blocked tasks without checking `fest deps` first
