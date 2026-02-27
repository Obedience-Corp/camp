---
name: camp-projects
description: Manage campaign project submodules - add repos, commit with auto-sync, check status, pull/push across projects, and manage worktrees. Use when working with project repositories in a campaign.
---

# Campaign Project Management

Projects are git submodules managed by camp. The key command is `camp p commit` which keeps submodule refs in sync automatically.

## Adding Projects

```bash
camp project add <git-url>           # Add a repo as a project submodule
camp project add <url> --name foo    # Add with custom name
camp project list                    # List all projects
camp project remove <name>           # Remove a project submodule
```

Projects live under `projects/` in the campaign root.

## Committing in Projects (Critical)

**Always use `camp p commit` instead of raw `git commit` when working in project submodules.**

```bash
camp p commit -m "fix: resolve auth bug"
```

This command:
1. Auto-stages all changes (`-a` by default)
2. Creates the commit in the project repo
3. Auto-syncs the submodule ref at the campaign root (`--sync` by default)

This saves a manual step and keeps the campaign root in sync with the project's HEAD.

### Why This Matters

Raw `git commit` in a submodule creates the commit but does NOT update the submodule ref in the parent campaign. This causes the campaign root to show the submodule as "dirty" and other developers will clone an older version of your project.

## Status Across Projects

```bash
camp status                  # Git status for current project
camp status all              # Git status across ALL projects
camp status all --view       # TUI view of all project statuses
```

## Pull and Push

```bash
camp pull                    # Pull current project
camp pull all                # Pull all projects
camp push                    # Push current project
camp push all                # Push all projects
```

## Worktrees

Git worktrees let you work on multiple branches simultaneously without switching:

```bash
camp project worktree add <project> <branch>   # Create worktree
camp project worktree list                      # List all worktrees
camp project worktree remove <path>             # Remove worktree
```

Worktrees live under `projects/worktrees/`.

## Common Mistakes

- Using raw `git add . && git commit` in a submodule - breaks submodule ref sync. Always use `camp p commit`.
- Forgetting `camp pull all` before starting work - may miss upstream changes in other projects
- Creating worktrees manually with `git worktree add` - camp won't track them. Use `camp project worktree add`.
