<p align="center">
  <img src="docs/images/banner.jpg" alt="camp banner" width="400">
</p>

# camp

> **One place for all your context and all your work.** Part of [Festival](https://github.com/Obedience-Corp/festival). Camp handles the workspace — your projects, tools, intents, and context. [fest](https://github.com/Obedience-Corp/fest) handles the planning and execution inside it.

Campaign workspace manager — group every project, tool, and piece of context you care about into a single campaign, and navigate between them instantly.

## Features

- **Navigation** - Category shortcuts, fuzzy finding, and pins (`go`, `pin`, `shortcuts`)
- **Project Management** - Git submodules, linked local workspaces, worktrees, and scaffolding (`project add/link/list/new/remote/remove/run/unlink/worktree`)
- **Planning** - Intents, status flows, dungeon for deprioritized work (`intent`, `flow`, `dungeon`, `gather`)
- **Productivity** - Leverage scoring to identify high-impact work (`leverage`)
- **Git Integration** - Campaign-level git operations (`commit`, `log`, `push`, `status`)
- **Campaign Ops** - Health checks, file operations, cross-campaign tools (`doctor`, `copy`, `move`, `sync`)
- **Shell Integration** - Native cd behavior with zsh, bash, and fish (`shell-init`)
- **Tab Completion** - Smart completion for categories, projects, and paths

## Installation

### Go Install

```bash
go install github.com/Obedience-Corp/camp@latest
```

### From Source

```bash
git clone https://github.com/Obedience-Corp/camp
cd camp
just install stable
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

| Shortcut | Directory              | Description            |
|----------|------------------------|------------------------|
| `p`      | projects/              | Project subdirectories |
| `f`      | festivals/             | Festival methodology   |
| `w`      | workflow/              | Workflow directory     |
| `ai`     | ai_docs/               | AI documentation       |
| `d`      | docs/                  | Human documentation    |
| `i`      | .campaign/intents/     | Intents via `camp intent` |
| `wt`     | projects/worktrees/    | Git worktrees          |
| `du`     | dungeon/               | Archived work          |
| `cr`     | workflow/code_reviews/ | Code review materials  |
| `pi`     | workflow/pipelines/    | CI/CD pipelines        |
| `de`     | workflow/design/       | Design documents       |
| `ex`     | workflow/explore/      | Exploratory notes      |

`cgo i` remains available as an operator shortcut into the hidden intent state,
but the normal human interface is `camp intent`.

## Commands

### Navigation - `cgo`

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

**Pins**: Pin frequently visited directories, jump to them with `camp go` or `cgo`, then use toggle to bounce back:

```bash
camp pin code                               # Pin current directory
camp pin design workflow/design/my-project  # Pin a matching design directory
camp go code                                # Resolve a pin through camp go
cgo design                                  # Shell jump to a pin
cgo t                                       # Jump back to the previous location
camp pins                                   # List all pins
camp unpin design                           # Remove a pin
```

**Shortcuts**: View all category shortcuts and custom shortcuts:

```bash
camp shortcuts       # List all available shortcuts
```

### Setup

```bash
camp init                  # Initialize current directory
camp init my-campaign      # Create and initialize new directory
camp clone <url>           # Clone a campaign with full submodule setup
```

### Project Management

A **project** in camp is either:

- a **git submodule** tracked under `projects/`, shared through the campaign repository, or
- a **linked local workspace** — a symlink under `projects/` pointing to an
  external directory on the same machine. Linked projects are machine-local
  (the symlink and its `.camp` marker are not versioned by the campaign).

Both kinds behave the same for navigation, tab completion, and most project
subcommands. Use submodules when the project should travel with the campaign;
use links to pull an existing local repo into your campaign without moving or
copying it.

#### Git Submodule Projects

```bash
camp project add <url>                  # Clone a remote repo as a submodule
camp project add --local <path>         # Add an existing local repo as a submodule
camp project new <name>                 # Scaffold a new project submodule
camp project list                       # List all projects (submodules + links)
camp project remove <name>              # Remove a project (submodule or link)
```

`camp project add` also accepts `--campaign <name-or-id>` to target a
registered campaign other than the current one, and `--name` to override the
default project name.

#### Linked Local Workspaces

`camp project link` adds an existing directory on your machine to the campaign
as a symlink under `projects/<name>`, with a `.camp` marker written into the
linked directory recording the campaign ID.

```bash
camp project link                       # Link the current directory
camp project link ~/code/my-project     # Link another directory
camp project link ~/code/my-project --name backend   # Override the project name

# Outside a campaign, pick a registered target campaign:
camp project link --campaign platform
camp project link ~/code/my-project --campaign        # Interactive picker

camp project unlink                     # Remove the current linked project
camp project unlink my-project          # Remove by name
camp project unlink my-project --dry-run  # Preview without changing anything
```

`unlink` removes the symlink and cleans up the `.camp` marker; it never
touches the contents of the external directory.

#### Working Inside Projects

```bash
camp project commit -m "Fix bug"        # Commit inside the current project
camp project commit --project camp -m "Fix bug"   # Explicit project target
camp project run -p camp just test      # Run a command inside a project
camp project prune --project camp       # Prune merged local branches
camp project remote                     # Manage per-project remotes
camp project worktree add feature -p camp         # Create a worktree
```

All project commands accept `--project / -p <name>` with tab completion to
target a project by name; when omitted, camp auto-detects from your current
working directory.

Monorepo subprojects are addressable with `@` syntax (e.g.,
`obey-platform-monorepo@obey`).

### Planning

Intents, status flows, and the dungeon provide lightweight planning tools:

```bash
# Intents - capture ideas, goals, and work items
camp intent                # Manage campaign intents
camp intent add --campaign other-campaign "Capture idea"  # Cross-campaign capture (add only)
camp gather                # Import external data into the intent system

# Flows - track work status
camp flow                  # Manage status workflows for organizing work

# Dungeon - archive deprioritized work
camp dungeon               # Move items to/from the dungeon

# Work items - a unified dashboard across intents, designs, explore, and festivals
camp workitem              # Interactive TUI dashboard of active work
camp workitem --json       # Machine-readable output
```

### Productivity

```bash
# Leverage scoring - identify high-impact work
camp leverage              # Compute leverage scores for campaign projects
```

See [docs/leverage-score.md](docs/leverage-score.md) for details on the scoring algorithm.

### Git Integration

Campaign-level git operations:

```bash
camp commit                # Commit changes in the campaign root
camp log                   # Show git log of the campaign
camp push                  # Push campaign changes to remote
camp push all              # Push all submodules with unpushed changes
camp pull                  # Pull latest changes
camp pull all              # Pull all submodules
camp status                # Show git status of the campaign
camp status all            # Dashboard of all submodules (branch, dirty/clean, push status, unmerged branches)
camp status all --view     # Interactive TUI viewer with per-repo detail
camp fresh                 # Post-merge branch cycling: checkout default, pull, prune, optional new branch
camp fresh all             # Same cycle across every project in the campaign
```

### Campaign Operations

```bash
camp doctor                # Diagnose and fix campaign health issues
camp sync                  # Safely synchronize submodules
camp refs-sync             # Update campaign's recorded submodule pointers to each submodule's HEAD
camp copy                  # Copy a file or directory within the campaign
camp move                  # Move a file or directory within the campaign
camp run                   # Execute command from campaign root, or just recipe in a project
camp root                  # Print the current campaign root
camp id                    # Print the current campaign ID
camp concepts              # List configured concepts (picker/completion concepts)
```

### Global Commands

```bash
camp list                  # List all registered campaigns
camp switch                # Switch to a different campaign
camp transfer              # Copy files between campaigns
camp register              # Register campaign in global registry
camp unregister            # Remove campaign from registry
camp registry              # Maintain ~/.obey/campaign/registry.json (prune, sync, check)
```

### Skills

Camp centralizes skill bundles in `.campaign/skills/` and projects them into
tool ecosystems (Claude, agents, etc.) as per-bundle symlinks so a single
source of truth stays in the campaign while provider-native skills
directories keep working.

```bash
camp skills                # Manage campaign skill bundle projection (link/unlink/status)
```

### System

```bash
camp settings              # Manage camp configuration (interactive)
camp date                  # Append date suffix to file or directory name
camp version               # Show version information
```

### Shell Integration

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
cgo <TAB>                # Completes categories: p f w a d i wt du cr pi de
cgo p <TAB>              # Completes project names

# 3. Tab completion for camp commands
camp <TAB>               # Completes: init go project list register...
camp project <TAB>       # Completes: add commit link list new prune remote remove run unlink worktree
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

## Campaign Directory Structure

A campaign provides a standardized layout for AI development:

```
my-campaign/
├── .campaign/           # Campaign configuration and system state
│   ├── campaign.yaml
│   ├── watchers.yaml
│   ├── intents/         # System-managed intents (camp intent, cgo i)
│   │   ├── inbox/
│   │   ├── active/
│   │   ├── ready/
│   │   └── dungeon/
│   ├── settings/        # Campaign-local settings and defaults
│   ├── skills/          # Campaign skill bundles (camp skills)
│   ├── leverage/        # Leverage snapshots and cache (camp leverage)
│   └── cache/           # Navigation index cache
├── projects/            # Git submodules and linked workspaces
│   ├── api-service/     # Git submodule
│   ├── web-app/         # Git submodule
│   ├── my-local-repo -> /Users/you/code/my-local-repo   # Linked workspace (symlink)
│   └── worktrees/       # Git worktrees (cgo wt)
│       └── api-service/
│           ├── feature-x/
│           └── bugfix-y/
├── festivals/           # Festival methodology (cgo f)
│   ├── planning/
│   ├── active/
│   ├── ready/
│   ├── ritual/
│   └── dungeon/         # completed/, archived/, someday/
├── workflow/            # Workflow resources (cgo w)
│   ├── code_reviews/    # Review notes (cgo cr)
│   ├── pipelines/       # CI/CD configs (cgo pi)
│   └── design/          # Design documents (cgo de)
├── ai_docs/             # AI documentation (cgo ai)
├── docs/                # Human documentation (cgo d)
└── dungeon/             # Archived work (cgo du)
```

## Worktree Navigation

Navigate git worktrees with `@` syntax:

```bash
cgo wt                    # Jump to worktrees/
cgo wt api-service@       # Show branches for api-service
cgo wt api-service@feat   # Jump to api-service@feature-x
```

## Tab Completion

The shell integration includes intelligent tab completion:

```bash
# Navigation
cgo <TAB>                              # Shows: p f w ai d i wt du cr pi de ex
cgo p <TAB>                            # Shows: api-service web-app cli-tool
cgo p api<TAB>                         # Completes to: api-service api-gateway
cgo wt api@<TAB>                       # Shows worktree branches

# --project flag (all project commands)
camp project commit -p <TAB>           # Shows project names from project list
camp project worktree add -p <TAB>     # Same project name completion
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

### Config Files

Camp uses a small set of configuration and state files:

- `~/.obey/campaign/config.json` for global user preferences such as editor,
  theme, `no_color`, and `verbose`
- `.campaign/campaign.yaml` for campaign metadata, project entries, and picker
  concepts
- `.campaign/settings/jumps.yaml` for campaign-local navigation paths and
  shortcuts
- `.campaign/settings/fresh.yaml` for optional `camp fresh` defaults
- `.campaign/watchers.yaml` for the camp/fest watcher contract

See [docs/campaign-settings-files.md](docs/campaign-settings-files.md) for the
full file-by-file reference, including which files are scaffolded by
`camp init` and which are only created on first use.

## Documentation

- [CLI Reference](docs/cli-reference/camp-reference.md) - Complete reference for every command and flag
- [`.campaign/` Directory Reference](docs/campaign-directory-reference.md) - Hidden campaign metadata layout and ownership
- [Campaign Settings Files](docs/campaign-settings-files.md) - Global and local config/state files explained
- [Leverage Scoring](docs/leverage-score.md) - How leverage scores are computed
- [Shortcuts](docs/SHORTCUTS.md) - Category shortcuts reference
- [Shell Integration](docs/shell-integration.md) - Detailed shell setup guide

Individual command docs are in [`docs/cli-reference/`](docs/cli-reference/) (auto-generated via `just docs`).

## Development

```bash
just                      # List all commands
just build-camp           # Build camp binary (vet + build)
just build                # Show all build recipes (profiles, cross-platform)
just test                 # Show all test recipes
just test all             # Run all tests
just install              # Show install options (stable, dev, current)
just install stable       # Install stable profile to $GOBIN
just docs                 # Regenerate CLI reference docs
just run <args>           # Run with arguments
```

## Part of Festival

Camp is one half of [Festival](https://github.com/Obedience-Corp/festival), the current product from [Obedience Corp](https://github.com/Obedience-Corp).

- **camp** — workspace and context. One place for all your projects, tools, intents, agents, and work.
- **[fest](https://github.com/Obedience-Corp/fest)** — planning and execution. Hierarchical: festival → phase → sequence → task.
- **[Festival](https://github.com/Obedience-Corp/festival)** — combined distribution of camp + fest. Full docs at [fest.build](https://fest.build).

## License

Functional Source License 1.1 (FSL-1.1-ALv2) - See [LICENSE](LICENSE) for details.
