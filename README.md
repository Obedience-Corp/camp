![camp banner](docs/images/banner.jpg)

# camp

Fast, fuzzy navigation for multi-project AI workspaces.

## Features

- **Category Shortcuts** - Jump to any directory with single-letter shortcuts (p=projects, f=festivals)
- **Fuzzy Finding** - Type partial names and camp finds the match
- **Campaign Structure** - Standardized directory layout for AI development
- **Shell Integration** - Native cd behavior with zsh, bash, and fish
- **Tab Completion** - Smart completion for categories, projects, and paths

## Installation

### Go Install

```bash
go install github.com/obediencecorp/camp@latest
```

### From Source

```bash
git clone https://github.com/obediencecorp/camp
cd camp
just install
```

### Shell Integration

Add to your shell config to enable the `cgo` navigation function and tab completion:

**Zsh** (~/.zshrc):
```bash
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

The eval hook provides:
- **`cgo` function** - Shell-native navigation with actual `cd` behavior
- **Tab completion** - Context-aware completion for categories, projects, and commands
- **`camp` completion** - Full command completion for the camp CLI

After adding the eval line, restart your shell or run `source ~/.zshrc` (or equivalent).

## Quick Start

```bash
# 1. Initialize a campaign
mkdir my-campaign && cd my-campaign
camp init

# 2. Add shell integration (restart shell after)
echo 'eval "$(camp shell-init zsh)"' >> ~/.zshrc

# 3. Navigate!
cgo p          # Jump to projects/
cgo f          # Jump to festivals/
cgo p api      # Fuzzy find "api" in projects/
```

## Category Shortcuts

Navigate instantly with single-letter shortcuts:

| Shortcut | Directory      | Description                |
|----------|----------------|----------------------------|
| `p`      | projects/      | Project subdirectories     |
| `c`      | corpus/        | Reference materials        |
| `f`      | festivals/     | Festival methodology       |
| `a`      | ai_docs/       | AI documentation           |
| `d`      | docs/          | Human documentation        |
| `w`      | worktrees/     | Git worktrees              |
| `r`      | code_reviews/  | Code review materials      |
| `pi`     | pipelines/     | CI/CD pipelines            |

## Commands

### cgo - Navigate

The `cgo` shell function is your primary interface:

```bash
# Jump to campaign root
cgo

# Jump to category
cgo p               # projects/
cgo f               # festivals/

# Fuzzy search within category
cgo p api           # projects/api-* (matches api-service, api-gateway, etc.)
cgo f fest          # festivals/*fest*

# Run command from category (without changing directory)
cgo -c p ls           # List contents of projects/
cgo -c f fest status  # Run fest status from festivals/
```

### camp init

Initialize a new campaign:

```bash
camp init                  # Initialize current directory
camp init my-campaign      # Create and initialize new directory
camp init --name "My Project"  # Set campaign name
```

### camp go

Lower-level navigation (use `cgo` for shell integration):

```bash
camp go p             # Print cd command for projects/
camp go p --print     # Print just the path
camp go p -c ls       # Run command from category
```

### camp project

Manage projects within the campaign:

```bash
camp project add <url>    # Add git submodule
camp project list         # List all projects
camp project remove <name>  # Remove a project
```

### camp shell-init

Generate shell integration scripts:

```bash
camp shell-init zsh       # Output zsh init script
camp shell-init bash      # Output bash init script
camp shell-init fish      # Output fish init script
```

#### How the Eval Hook Works

The eval hook dynamically generates and executes shell code at startup:

```bash
# What happens when you add this to ~/.zshrc:
eval "$(camp shell-init zsh)"

# 1. camp shell-init zsh outputs shell code (functions, completions)
# 2. eval executes that code in your current shell
# 3. The cgo function and completions become available
```

#### Why Eval Instead of Sourcing a File?

- **Version sync** - Always uses functions matching your installed camp version
- **No file management** - Nothing to update when camp is upgraded
- **Shell detection** - Camp can detect your shell environment dynamically

#### What Gets Installed

The shell-init script provides:

```bash
# 1. The cgo navigation function
cgo p                    # Runs: cd "$(camp go p --print)"
cgo p api                # Runs: cd "$(camp go p api --print)"
cgo -c p ls              # Runs: camp go p -c ls (no cd)

# 2. Tab completion for cgo
cgo <TAB>                # Completes categories: p c f a d w r pi
cgo p <TAB>              # Completes project names

# 3. Tab completion for camp commands
camp <TAB>               # Completes: init go project list register...
camp project <TAB>       # Completes: add list remove
```

#### Troubleshooting

```bash
# Verify camp is in PATH
which camp

# Test shell-init output
camp shell-init zsh

# Manually reload
source ~/.zshrc

# Check if cgo is defined
type cgo
```

### camp complete

Generate completion candidates (used by shell integration):

```bash
camp complete             # Category shortcuts
camp complete p           # Projects in category
camp complete p api       # Projects matching "api"
```

## Campaign Directory Structure

A campaign provides a standardized layout for AI development:

```
my-campaign/
├── .campaign/           # Campaign configuration
│   └── campaign.yaml
├── projects/            # Git submodules
│   ├── api-service/
│   └── web-app/
├── festivals/           # Festival methodology
│   ├── active/
│   ├── planned/
│   └── completed/
├── ai_docs/             # AI documentation
├── docs/                # Human documentation
├── corpus/              # Reference materials
├── worktrees/           # Git worktrees
│   └── api-service/
│       ├── feature-x/
│       └── bugfix-y/
├── code_reviews/        # Review notes
└── pipelines/           # CI/CD configs
```

## Worktree Navigation

Navigate git worktrees with `@` syntax:

```bash
cgo w                     # List all worktrees
cgo w api-service@        # Show branches for api-service
cgo w api-service@feat    # Jump to api-service@feature-x
```

## Tab Completion

The shell integration includes intelligent tab completion:

```bash
cgo <TAB>           # Shows: p c f a d w r pi
cgo p <TAB>         # Shows: api-service web-app cli-tool
cgo p api<TAB>      # Completes to: api-service api-gateway
cgo w api@<TAB>     # Shows worktree branches
```

## Configuration

### Campaign Config

Located at `.campaign/campaign.yaml`:

```yaml
name: my-campaign
type: product
description: My awesome project
```

### Project Jump Locations

Projects can define shortcuts to jump directly to subdirectories within the project. The `default` shortcut is used when navigating to the project without specifying a sub-path.

Add a `projects` section to `.campaign/campaign.yaml`:

```yaml
projects:
  - name: festival-methodology
    path: projects/festival-methodology
    shortcuts:
      default: fest/           # Jump here by default
      cli: fest/cmd/fest/      # Named sub-shortcut

  - name: api-service
    path: projects/api-service
    shortcuts:
      default: src/
```

Usage:

```bash
cgo p fest       # Jumps to projects/festival-methodology/fest/ (uses default)
cgo p fest cli   # Jumps to projects/festival-methodology/fest/cmd/fest/
cgo p api        # Jumps to projects/api-service/src/ (uses default)
```

Without a `default` shortcut, navigation jumps to the project root.

### Global Config

Located at `~/.config/obey/campaign/config.yaml`:

```yaml
default_type: product
editor: code
```

## Development

```bash
# Build
just build

# Run tests
just test

# Run with arguments
just run <args>

# Install locally
just install
```

## License

MIT
