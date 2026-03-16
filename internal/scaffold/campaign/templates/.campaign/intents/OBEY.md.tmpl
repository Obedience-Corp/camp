# Intents

System-owned intent state managed through `camp intent`.

## Purpose

`.campaign/intents/` is camp's canonical storage root for captured intents and
their status buckets.

Use `camp intent` and `camp gather` to create, move, review, and promote items
here. Treat this directory as camp-managed state, not as a second human-owned
`workflow/` notebook.

This directory is pre-scaffolded because camp needs a stable intent inbox even
though the normal interface is the CLI.

## Relationship To Other Planning Workflows

- `camp intent` captures ideas quickly with minimal commitment and stores them
  under `.campaign/intents/`.
- `workflow/explore/` investigates uncertain topics and prototypes.
- `workflow/design/` develops concrete designs for work you plan to implement.
- `festivals/` handles structured hierarchical plans.

Any of these can become inputs to a future festival ingest or planning phase.

## What Goes Here

- Intent files created through `camp intent add`
- Gathered or imported work items managed by camp commands
- Status transitions handled by `camp intent move` and `camp intent promote`
- Supporting audit/state files maintained by camp alongside the intent buckets

Avoid treating this directory as a long-lived freeform notebook. If you need
general planning material, use `workflow/explore/`, `workflow/design/`, or
`docs/`.

## Lifecycle

Intents progress through status directories:

1. **inbox/** - Captured but not reviewed
2. **ready/** - Reviewed/enriched, ready for promotion
3. **active/** - Promoted to festival or design doc, work in progress
4. **dungeon/done/** - Completed
5. **dungeon/killed/** - Abandoned
6. **dungeon/archived/** - Preserved but inactive
7. **dungeon/someday/** - Deferred

## Usage

```bash
# Capture a new intent (fast, sub-second)
camp intent add

# Capture with editor for deep context
camp intent add --edit

# List all intents
camp intent list

# Edit an existing intent
camp intent edit [id]

# Move to a new status
camp intent move [id] ready

# Move to dungeon with required reason
camp intent move [id] archived --reason "preserve for reference"

# Promote to a Festival
camp intent promote [id]
```

## Structure

```text
.campaign/intents/
├── inbox/             # Captured, not reviewed
├── ready/             # Reviewed and ready for promotion
├── active/            # Work in progress
└── dungeon/
    ├── done/          # Completed
    ├── killed/        # Abandoned
    ├── archived/      # Preserved
    └── someday/       # Deferred
```

## Intent File Format

Each intent is a markdown file with YAML frontmatter:

```markdown
---
id: 20260119-153412-add-dark-mode
title: Add dark mode toggle
type: feature
status: inbox
created_at: 2026-01-19
---

# Add dark mode toggle

## Description
...
```

## Design Principle

> **Capture should never require commitment.**
> If an idea requires structure before it can be saved, it will be lost.
> The system allows ideas to exist in an unrefined state and earn their
> complexity later.
