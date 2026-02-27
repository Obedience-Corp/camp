---
name: campaign-workflows
description: Work with campaign intents, dungeons, and status flows - capture ideas, track lifecycle, archive completed work, and promote intents to festivals. Use when managing ideas, archiving work, or organizing workflow items.
---

# Campaign Workflows

Campaigns have three interconnected workflow systems: **intents** (idea capture), **dungeons** (archival), and **flows** (status tracking).

## Intents - Idea Capture and Tracking

Intents capture ideas, bugs, features, and research topics that aren't ready to become festivals yet.

### Intent Lifecycle

```
inbox → active → ready → promoted to festival
  ↘      ↘        ↘
  dungeon/killed (abandoned)
  dungeon/someday (deferred)
  dungeon/done (resolved)
```

### Key Commands

```bash
camp intent add "Add dark mode toggle"          # Quick capture to inbox
camp intent add -e "Refactor auth system"        # Open editor for full context
camp intent list                                 # List all intents
camp intent list --status active                 # Filter by status
camp intent show <name>                          # View intent details
camp intent edit <name>                          # Edit in editor (fuzzy match)
camp intent move <name> active                   # Move to active status
camp intent promote <name>                       # Promote to a Festival
camp intent archive <name>                       # Archive intent
camp intent find "auth"                          # Search by title/content
camp intent count                                # Count intents by status
```

### Intent Principle

"Capture should never require commitment." Ideas can exist in raw form (inbox) and earn their complexity later. Only promote to a festival when the intent is clear, scoped, and ready for structured execution.

### Intent Gathering

Gather related intents into a unified document:

```bash
camp intent gather                               # Gather related intents
camp gather feedback                             # Import external feedback
```

## Dungeons - Archival System

The dungeon is where work goes when it reaches a terminal state. Every dungeon has the same subdirectory structure:

```
dungeon/
├── completed/     # Successfully finished
├── archived/      # Preserved but won't revisit
└── someday/       # Deferred, might revisit later
```

Dungeons appear at multiple levels:
- `dungeon/` - Campaign-level archive
- `workflow/intents/dungeon/` - Intent archive (adds `done/` and `killed/`)
- `workflow/code_reviews/dungeon/` - Review archive
- `festivals/dungeon/` - Festival archive

### Dungeon Crawl

Periodically review dungeon contents to clean up or resurface work:

```bash
camp dungeon crawl      # Interactive review of dungeon items
```

This walks through items and lets you keep, move, or delete them.

## Flows - Status Tracking

Flows define status directories that items move between, with transition tracking.

### Default Flow Structure

```
<flow-directory>/
├── .workflow.yaml       # Flow configuration
├── (items at root)      # Active/in-progress work
└── dungeon/
    ├── completed/
    ├── archived/
    └── someday/
```

### Key Commands

```bash
camp flow list                    # List registered flows
camp flow items                   # List items in current flow
camp flow status                  # Show workflow statistics
camp flow move <item> <status>    # Move item to new status
camp flow show                    # Display workflow structure
camp flow sync                    # Create missing directories from schema
```

### Moving Items Through Flows

```bash
camp flow move my-design completed          # Mark as done
camp flow move old-spec dungeon/archived    # Archive it
camp flow move idea dungeon/someday         # Defer for later
```

## Common Mistakes

- Creating festivals for small ideas - use intents first, promote when ready
- Deleting old work instead of moving to dungeon - dungeon preserves history
- Skipping `camp dungeon crawl` - dungeons accumulate clutter without periodic review
- Manually creating status directories - use `camp flow sync` to match schema
