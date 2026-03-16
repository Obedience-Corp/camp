# Shell Integration

Camp provides shell integration through the `cgo` function, which allows instant navigation to campaign directories without needing to type `cd` or wait for a subprocess to complete.

## Quick Setup

Add one of the following to your shell configuration:

**Zsh** (~/.zshrc):
```zsh
eval "$(camp shell-init zsh)"
```

**Bash** (~/.bashrc):
```bash
eval "$(camp shell-init bash)"
```

**Fish** (~/.config/fish/config.fish):
```fish
camp shell-init fish | source
```

## The `cgo` Function

The `cgo` (camp-go) function is the primary interface for navigation. It's a shell function (not an alias or script) because it needs to change the current working directory.

### Basic Usage

```bash
# Jump to campaign root
cgo

# Jump to a category
cgo p         # projects/
cgo f         # festivals/
cgo i         # workflow/intents/
cgo d         # docs/
cgo ai        # ai_docs/
cgo w         # workflow/
cgo wt        # projects/worktrees/
cgo cr        # workflow/code_reviews/
cgo pi        # workflow/pipelines/
cgo de        # workflow/design/
cgo ex        # workflow/explore/

# Fuzzy search within category
cgo p api     # projects/api-* (fuzzy match)
cgo f fest    # festivals/*fest* (fuzzy match)
```

### Category Shortcuts

| Shortcut | Directory      | Description             |
|----------|----------------|-------------------------|
| p        | projects/      | Project directories     |
| f        | festivals/     | Festival planning       |
| i        | workflow/intents/ | Intents              |
| d        | docs/          | Documentation           |
| ai       | ai_docs/       | AI documentation        |
| w        | workflow/      | Workflow resources      |
| wt       | projects/worktrees/ | Git worktrees     |
| cr       | workflow/code_reviews/ | Code review materials |
| pi       | workflow/pipelines/ | CI/CD pipelines     |
| de       | workflow/design/ | Design documents       |
| ex       | workflow/explore/ | Exploratory notes     |

### Running Commands

Use `-c` to run a command from a category directory without changing to it:

```bash
# List projects
cgo -c p ls

# Run fest status from festivals
cgo -c f fest status

# Run tests from a project
cgo -c p api make test
```

The command inherits your current environment and its output goes to your terminal.

## Tab Completion

Shell integration includes intelligent tab completion:

```bash
cgo <TAB>       # Shows category shortcuts
cgo p <TAB>     # Shows projects in projects/
cgo p ap<TAB>   # Completes to matching project names
```

The completion system queries camp for real-time suggestions based on your campaign structure.

## Helper Functions

Shell integration also provides shorthand functions for common operations:

### `cint` - Quick Intent Capture

```bash
cint "my idea for a new feature"
```

Equivalent to `camp intent add "..."`. Quickly capture thoughts and ideas.

### `cie` - Explore Intents

```bash
cie
```

Equivalent to `camp intent explore`. Opens the interactive TUI for browsing, filtering, and managing intents.

### `cr` - Run from Campaign Root

```bash
cr make test
```

Equivalent to `camp run make test`. Runs a command from the campaign root directory.

## Technical Details

### Why a Shell Function?

The `cgo` command must be a shell function because:
- Only shell functions can change the current directory
- Scripts and binaries run in subprocesses and cannot affect the parent shell
- Aliases don't support the complex logic needed for argument handling

### How It Works

1. `cgo` calls `camp go --print` to get the target path
2. If successful, it runs `cd` to change to that directory
3. If there's an error, it shows the error message

```bash
# What happens internally:
cgo p api
# → dest=$(camp go p api --print)
# → cd "$dest"
```

### Error Handling

```bash
# Not in a campaign
$ cgo
Error: not in a campaign
Hint: Initialize with 'camp init' or navigate to a campaign

# Target not found
$ cgo p nonexistent
camp: not found: p nonexistent

# Multiple matches show selection
$ cgo p api
Multiple matches found:
  api-service
  api-gateway
Using best match: api-service
```

## Shell-Specific Notes

### Zsh

- Uses `[[ ]]` for conditionals
- Uses `_describe` for completion
- `compdef` registers completion functions

### Bash

- Compatible with bash 3.2+ (macOS default)
- Uses `[ ]` for POSIX compatibility
- Avoids bash 4+ features (associative arrays, `${var,,}`)
- Uses `complete -F` for completion

### Fish

- Uses `test` instead of `[ ]` or `[[ ]]`
- Uses `set -l` for local variables
- Uses `$status` instead of `$?`
- Array syntax: `$argv[1]`, `$argv[2..-1]`
- Excellent built-in completion support

## Troubleshooting

### cgo not found

Make sure you added the shell-init line to your config and restarted your shell:

```bash
# Check if camp is available
which camp

# Re-source your config
source ~/.zshrc  # or ~/.bashrc, config.fish
```

### Completion not working

```bash
# Zsh: Make sure compinit is loaded
autoload -Uz compinit && compinit

# Bash: Check if bash-completion is installed
type _init_completion
```

### Permission denied on cd

The target directory might not have read/execute permissions:

```bash
# Check permissions
ls -la $(camp go p --print)

# Fix if needed
chmod +x path/to/directory
```

## Advanced Usage

### Use with other tools

```bash
# Open project in editor
code $(camp go p api --print)

# Fuzzy search with fzf
camp go p --print | fzf | xargs cd

# Copy path to clipboard
camp go p api --print | pbcopy
```

### Custom shortcuts

The shell integration supports custom shortcut mappings (future feature). For now, you can add aliases:

```bash
alias cdp='cgo p'
alias cdf='cgo f'
```
