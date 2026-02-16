# {{.Name}}

{{.Description}}

## Purpose

This workflow organizes work using a dungeon-centric model where the root
directory holds active work, and the `dungeon/` directory holds everything else.

## Directory Structure

```
./
├── OBEY.md              # This file
├── .workflow.yaml       # Workflow configuration
├── (your active work)   # Items you're working on right now
└── dungeon/             # All non-active statuses
    ├── OBEY.md
    ├── ready/           # Queued and ready to start
    ├── completed/       # Successfully finished
    ├── archived/        # Preserved for history
    └── someday/         # Deferred, low priority
```

## Common Commands

```bash
camp flow status               # View workflow overview
camp flow list                 # List active items
camp flow list dungeon/ready   # List items ready to start
camp flow move item dungeon/completed  # Mark item as done
camp flow crawl                # Review items interactively
```

## Workflow

Active work lives at the root level. When work is done, move it into the
appropriate dungeon subdirectory. To start something new, move it from
`dungeon/ready` to the root.
