---
name: campaign-commit
description: Choose the correct commit command in a campaign context. Use before committing code to determine whether to use camp p commit, fest commit, or standard git commit.
---

# Campaign Commit Workflow

Campaigns have multiple commit commands for different contexts. Using the wrong one breaks tracking and sync.

## Decision Tree

**1. Am I in a project submodule?**
Use `camp p commit -m "your message"`

This auto-stages changes and auto-syncs the submodule ref at the campaign root. Raw `git commit` in a submodule will NOT update the parent campaign's submodule pointer.

**2. Am I working on a festival task?**
Use `fest commit`

This creates a git commit with festival traceability metadata - task references, progress tracking, and metrics. It knows which task you're working on and adds the appropriate context.

**3. Am I at the campaign root?**
Use standard `git commit` with a descriptive message.

The campaign root tracks submodule refs and campaign-level files. Standard git workflow applies here.

## Quick Reference

| Context | Command | Why |
|---------|---------|-----|
| Project submodule | `camp p commit -m "msg"` | Auto-syncs submodule ref |
| Festival task | `fest commit` | Adds traceability metadata |
| Campaign root | `git commit -m "msg"` | Standard git |
| Both (submodule + festival) | `fest commit` then verify sync | fest commit handles both |

## Rules

- **Never** use `git add .` in a submodule - it can stage unintended files
- **Never** add "Co-authored-by: Claude" or similar tags to commit messages
- **Never** use `--no-verify` to skip pre-commit hooks without understanding why they failed
- Commit messages should be descriptive and focus on the "why"

## Examples

```bash
# Working in projects/camp on a bug fix
cd projects/camp
# ... make changes ...
camp p commit -m "fix: resolve navigation shortcut collision"

# Executing a festival task
# (fest commit automatically detects the current task context)
fest commit

# Campaign root changes (updated AGENTS.md, added a new workflow)
git add AGENTS.md workflow/design/new-spec.md
git commit -m "add API design spec for v2 auth flow"
```
