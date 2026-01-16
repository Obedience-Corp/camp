# Camp Examples

This directory contains example scripts demonstrating common camp workflows.

## Quick Start

New to camp? Start here:

```bash
bash quick-start.sh
```

## Examples

| Script | Description |
|--------|-------------|
| [quick-start.sh](quick-start.sh) | Installation and first campaign setup |
| [daily-workflows.sh](daily-workflows.sh) | Common navigation patterns for daily use |
| [project-management.sh](project-management.sh) | Adding, listing, and removing projects |
| [cross-campaign.sh](cross-campaign.sh) | Working with multiple registered campaigns |
| [scripting.sh](scripting.sh) | Using camp in shell scripts |
| [edge-cases.sh](edge-cases.sh) | Error handling and special scenarios |

## Running Examples

These scripts are educational - they show commands and expected output:

```bash
# View with comments explaining each step
bash quick-start.sh

# Or read directly
cat daily-workflows.sh
```

## Shell Integration

Most navigation uses the `cgo` shell function. Set it up with:

```bash
# zsh
eval "$(camp shell-init zsh)"

# bash
eval "$(camp shell-init bash)"

# fish
camp shell-init fish | source
```

## Category Shortcuts

| Shortcut | Directory | Description |
|----------|-----------|-------------|
| `p` | projects/ | Git submodules |
| `c` | corpus/ | Reference materials |
| `f` | festivals/ | Festival methodology |
| `a` | ai_docs/ | AI documentation |
| `d` | docs/ | Human documentation |
| `w` | worktrees/ | Git worktrees |
| `r` | code_reviews/ | Review notes |
| `pi` | pipelines/ | CI/CD configs |

## Need Help?

```bash
camp --help              # General help
camp go --help           # Navigation help
camp project --help      # Project management help
```
