---
name: campaign-structure
description: Understand campaign workspace structure - directory layout, concepts system, OBEY.md files, and what goes where. Use when you need to understand a campaign's organization, find where things belong, or orient yourself in the workspace.
---

# Campaign Structure

A campaign is a multi-project AI workspace with standardized organization. Understanding the structure is essential for working effectively.

## Directory Layout

```
campaign-root/
├── .campaign/              # Configuration and state
│   ├── campaign.yaml       # Name, type, mission, concepts
│   ├── settings/           # Jumps, pins, allowlist
│   ├── skills/             # Agent skill files (you're reading one)
│   └── cache/              # Navigation index
│
├── projects/               # Git submodule repositories
│   └── worktrees/          # Git worktrees for parallel work
│
├── festivals/              # Festival Methodology planning
│   ├── planning/           # Festivals being designed
│   ├── active/             # Currently executing
│   ├── ready/              # Prepared, awaiting execution
│   ├── ritual/             # Recurring processes
│   └── dungeon/            # Terminal states (completed, archived)
│
├── workflow/               # Development processes
│   ├── intents/            # Ideas and future work items
│   ├── code_reviews/       # Review notes and templates
│   ├── pipelines/          # CI/CD definitions
│   ├── design/             # Design documents and specs
│   └── explore/            # Experimental work
│
├── ai_docs/                # AI-generated documentation
│   ├── analysis/           # Code analysis outputs
│   ├── research/           # Research findings
│   ├── conversations/      # Notable AI conversations
│   └── generated/          # Generated docs
│
├── docs/                   # Human-authored documentation
│   ├── architecture/       # Architecture decisions
│   ├── api/                # API documentation
│   ├── guides/             # How-to guides
│   └── adr/                # Architecture Decision Records
│
├── dungeon/                # Campaign-level archive
│   ├── archived/           # Preserved but inactive
│   ├── completed/          # Successfully finished
│   └── someday/            # Deferred for later
│
├── AGENTS.md               # AI agent instructions for this campaign
└── CLAUDE.md               # Symlink to AGENTS.md
```

## Concepts

Concepts are named directory categories that power navigation. Each concept maps a name to a path with optional drilling depth.

| Concept | Path | What Lives Here |
|---------|------|----------------|
| `projects` | `projects/` | Git submodule repositories |
| `worktrees` | `projects/worktrees/` | Parallel branches via git worktree |
| `festivals` | `festivals/` | Festival plans and execution |
| `intents` | `workflow/intents/` | Ideas, bugs, features not yet festivals |
| `workflow` | `workflow/` | Code reviews, pipelines, design docs |
| `design` | `workflow/design/` | Design documents and specifications |
| `docs` | `docs/` | Human documentation |
| `dungeon` | `dungeon/` | Archived and deferred work |

Concepts are defined in `.campaign/campaign.yaml` and power the `cgo` navigation shortcuts.

## OBEY.md Files

Every major directory contains an `OBEY.md` file that explains:
- **What goes here** - Purpose and contents
- **Structure** - Expected subdirectory layout
- **Usage** - How to work with items in this directory
- **Navigation** - Shortcut to reach this directory

OBEY.md files are the "API documentation" for directories. When you encounter an unfamiliar directory, read its OBEY.md first.

## Where Things Go

| Need to... | Put it in... |
|-----------|-------------|
| Track an idea or bug not ready for planning | `workflow/intents/` |
| Plan and execute a project | `festivals/` |
| Store design documents or specs | `workflow/design/` |
| Keep code review notes | `workflow/code_reviews/` |
| Store CI/CD configs | `workflow/pipelines/` |
| Write user-facing documentation | `docs/` |
| Store AI research or analysis | `ai_docs/` |
| Archive finished or deferred work | `dungeon/` or the relevant directory's `dungeon/` |

## Key Distinction: festivals/ vs workflow/design/

- **festivals/** is for structured, phased project execution using the Festival Methodology. Use when work requires planning, sequencing, and progress tracking.
- **workflow/design/** is for design documents, specs, and explorations that inform decisions but don't require the full festival lifecycle.

## Campaign Configuration

`.campaign/campaign.yaml` contains:
- `name` - Campaign name
- `type` - product, research, tools, or personal
- `description` - What this campaign is for
- `mission` - Campaign's mission statement
- `concepts` - Navigation concept definitions

## Common Mistakes

- Creating new top-level directories instead of using the existing structure
- Putting design docs in `docs/` instead of `workflow/design/` (docs/ is for user-facing documentation)
- Not reading OBEY.md before adding files to a directory
- Confusing `dungeon/` (campaign-level archive) with `festivals/dungeon/` (festival-specific terminal states)
