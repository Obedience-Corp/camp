---
title: "camp CLI Reference"
weight: 1
---

# camp CLI Reference

---

## camp

Campaign management CLI for multi-project AI workspaces

### Synopsis

Camp manages multi-project AI workspaces with fast navigation.

Camp provides structure and navigation for AI-powered development workflows.
It creates standardized campaign directories, manages git submodules as projects,
and enables lightning-fast navigation through category shortcuts and TUI fuzzy finding.

GETTING STARTED:
  camp init               Initialize a new campaign in the current directory
  camp project list       List all projects in the campaign
  camp list               Show all registered campaigns

NAVIGATION (using cgo shell function):
  cgo                     Navigate to campaign root
  cgo p                   Navigate to projects directory
  cgo f                   Navigate to festivals directory
  cgo <name>              Fuzzy find and navigate to any target

COMMON WORKFLOWS:
  camp project add <url>  Add a git repo as a project submodule
  camp run <command>      Run command from campaign root directory
  camp shortcuts          View all available navigation shortcuts

Run 'camp shell-init' to enable the cgo navigation function.

```
camp [flags]
```

### Options

```
  -h, --help       help for camp
      --no-color   disable colored output
```
---

## camp artifacts

Manage declared artifact roots (.campaign/artifacts.yaml)

### Synopsis

Manage the campaign's declared artifact roots: directories of heavy non-git
payloads (media, renders, datasets) that 'camp sync --from <machine>' moves
between your machines with rsync instead of git.

The declaration file (.campaign/artifacts.yaml) is committed, so every
machine knows what belongs to the campaign. Declared roots should be
gitignored: a root that is also git-tracked would make the same bytes both
git content and artifact content. Manifests and per-peer sync snapshots are
machine-local derived state under .campaign/cache (gitignored).

### Examples

```
  camp artifacts list
  camp artifacts add media/renders
  camp artifacts add datasets --policy on-demand
  camp artifacts remove media/renders
  camp artifacts manifest media/renders
```

### Options

```
  -h, --help   help for artifacts
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp artifacts add

Declare an artifact root

### Synopsis

Declare a campaign-relative directory as an artifact root.

Policy 'always' (default) syncs the root on every 'camp sync --from
<machine>'; 'on-demand' syncs it only when artifacts are requested
explicitly (--artifacts-only).

```
camp artifacts add <path> [flags]
```

### Options

```
  -h, --help            help for add
      --policy string   Sync policy: always (every peer sync) or on-demand (--artifacts-only) (default "always")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp artifacts list

List declared artifact roots

```
camp artifacts list [flags]
```

### Options

```
  -h, --help   help for list
      --json   Output as JSON for scripting
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp artifacts manifest

Print a declared root's manifest as JSON

### Synopsis

Walk a declared artifact root and print its manifest (relative path, size,
mtime per file) as JSON. This is the same shape sync snapshots use, so it is
useful for scripting and for comparing roots across machines.

```
camp artifacts manifest <path> [flags]
```

### Options

```
  -h, --help   help for manifest
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp artifacts remove

Remove an artifact root declaration

### Synopsis

Remove a declared artifact root. Files on disk are not touched.

```
camp artifacts remove <path> [flags]
```

### Options

```
  -h, --help   help for remove
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp attach

Attach an external directory to a campaign

### Synopsis

Attach a non-project directory to a campaign by writing a .camp marker.

The user manages the symlink (if any). camp attach only writes the marker at
the resolved target so commands run from inside that directory know which
campaign owns it.

If the target is reached through a symlink, camp follows it once and writes
the marker at the final directory.

Campaign selection:
  - inside a campaign, omit --campaign to attach to the current campaign
  - outside a campaign in an interactive terminal, omit --campaign to pick
  - use a bare --campaign to force the picker even inside a campaign
  - use --campaign <name-or-id> for scripts or to skip the picker

Examples:
  camp attach docs/examples/external-repo
  camp attach ~/scratch/notes-link
  camp attach ~/scratch/notes-link --campaign
  camp attach /abs/path/to/dir --campaign platform

```
camp attach <path> [flags]
```

### Options

```
  -c, --campaign string   Target campaign by name or ID; omit value to pick interactively
      --force             Overwrite an existing attachment marker
  -h, --help              help for attach
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp audit

Inspect the campaign audit trail

### Synopsis

Inspect the campaign audit trail.

'camp audit doctor' scans linked repos for commits with no captured intent
linkage and reports them informationally. Untagged commits are a normal mode
for wrapper-opt-out workflows, not a violation.

### Options

```
  -h, --help   help for audit
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp audit backfill

Derive a source:backfill event stream from history (opt-in write)

### Synopsis

Derive ledger events from a campaign's existing history - tagged commits across
linked repos, intent frontmatter, and festival status histories - so a pre-ledger
campaign (or the pre-ledger history of this one) renders on the same timeline as
new activity.

Backfill is optional and never required. It is idempotent and live-wins: a fact
already captured live or by a prior backfill is skipped, so consecutive runs
produce zero new events. Dry-run by default; --apply writes the source:backfill
events into the standard shard layout.

```
camp audit backfill [flags]
```

### Options

```
      --apply   write the derived source:backfill events into the ledger
  -h, --help    help for backfill
      --json    emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp audit doctor

Scan linked repos for unattributed commits (informational)

### Synopsis

Scan the campaign root and every linked project repo, classifying each
commit as tagged, degraded, or untagged (no captured intent linkage).

Output is informational: untagged commits are surfaced, never scolded, and the
command exits 0 even when findings exist. Use --window to bound each repo to its
most recent N commits (default: full history).

```
camp audit doctor [flags]
```

### Options

```
  -h, --help         help for doctor
      --json         emit a structured JSON report
      --window int   scan only the most recent N commits per repo (0 = full history)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp audit reconcile

Fill ledger gaps from state files (opt-in write)

### Synopsis

Derive the events implied by campaign state files (intent statuses and
festival status histories), diff them against the ledger, and report the gaps -
facts the ledger does not yet capture. This covers users who never commit at all.

By default this is a dry run. Pass --apply to append the missing facts as
reconciled events (idempotent: reconciled ids are content-derived, so re-running
does not duplicate).

```
camp audit reconcile [flags]
```

### Options

```
      --apply   append the missing facts as reconciled events
  -h, --help    help for reconcile
      --json    emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp audit repair

Attribute a commit to a workitem or festival after the fact

### Synopsis

Attribute an already-landed commit to a workitem or festival by appending a
repaired event. This never rewrites git history; it only records the attribution
in the ledger (D004). Use it to claim an untagged commit surfaced by
'camp audit doctor' for a piece of work.

```
camp audit repair --sha <sha> (--workitem <id> | --festival <id>) --why <reason> [flags]
```

### Options

```
      --festival string   festival to attribute the commit to
  -h, --help              help for repair
      --repo string       evidence repo label (default: campaign-root)
      --sha string        commit sha to attribute (required)
      --why string        reason for the attribution (required)
      --workitem string   workitem to attribute the commit to
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp cache

Manage the navigation index cache

### Synopsis

Manage the navigation index cache used for fast project lookups.

```
camp cache [flags]
```

### Options

```
  -h, --help   help for cache
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp cache clear

Delete the navigation cache

### Synopsis

Delete the cached navigation index. It will be rebuilt on next navigation.

```
camp cache clear [flags]
```

### Options

```
  -h, --help   help for clear
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp cache info

Show cache status and metadata

### Synopsis

Show information about the navigation index cache including path, size, age, and staleness.

```
camp cache info [flags]
```

### Options

```
  -h, --help   help for info
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp cache rebuild

Force rebuild the navigation cache

### Synopsis

Force rebuild the navigation index cache, regardless of staleness.

```
camp cache rebuild [flags]
```

### Options

```
  -h, --help   help for rebuild
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp clone

Clone a campaign with full submodule setup

### Synopsis

Clone a campaign repository and initialize all submodules.

This command provides a single-step setup for new devices:

  1. CLONE REPOSITORY
     Clones the campaign repository with recursive submodules.

  2. SYNCHRONIZE URLs
     Copies URLs from .gitmodules to .git/config, ensuring
     URL consistency across all submodules.

  3. UPDATE SUBMODULES
     Fetches and checks out the correct commits for all submodules.

  4. VALIDATE SETUP
     Verifies all submodules are initialized, at correct commits,
     and have matching URLs.

  5. REGISTER CAMPAIGN
     If .campaign/campaign.yaml exists, registers the campaign
     in the global registry for navigation and discovery.

EXIT CODES:
  0  Success
  1  Runtime failure (clone failed before usable campaign)
  2  Usage error (bad flags or args)
  3  Partial success or validation failed

EXAMPLES:
  # Clone a campaign (default: SSH)
  camp clone git@github.com:Obedience-Corp/obey-campaign.git

  # Clone with HTTPS
  camp clone https://github.com/Obedience-Corp/obey-campaign.git

  # Clone to a specific directory
  camp clone git@github.com:org/repo.git my-campaign

  # Clone a specific branch
  camp clone git@github.com:org/repo.git --branch develop

  # Shallow clone (latest commit only)
  camp clone git@github.com:org/repo.git --depth 1

  # Clone without submodules
  camp clone git@github.com:org/repo.git --no-submodules

  # Clone without validation
  camp clone git@github.com:org/repo.git --no-validate

  # Clone without auto-registration
  camp clone git@github.com:org/repo.git --no-register

  # JSON output for scripting
  camp clone git@github.com:org/repo.git --json

  # Seed from a peer machine in ~/.obey/machines.yaml: root repo and
  # submodules clone from that machine's copy (LAN/tailnet), then origin is
  # re-pointed to the URL above and the delta fetched. The result is an
  # origin replica that arrived over the fast path; peer failures fall back
  # to plain origin cloning.
  camp clone git@github.com:org/repo.git --from studio-mac

```
camp clone <url> [directory] [flags]
```

### Options

```
  -b, --branch string   Clone specific branch (default: repository default branch)
      --depth int       Shallow clone depth (0 = full history)
      --from string     Seed git objects from this machine (id from ~/.obey/machines.yaml), then fetch the delta from origin
  -h, --help            help for clone
      --json            Output results as JSON for scripting
      --no-register     Skip auto-registration in global campaign registry
      --no-submodules   Skip submodule initialization
      --no-validate     Skip post-clone validation
  -p, --parallel int    Number of parallel submodule initializations (default 4)
  -v, --verbose         Show detailed output for each operation
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp commit

Commit changes in the campaign root

### Synopsis

Commit changes in the campaign root directory.

Automatically stages all changes and creates a commit. Handles
stale lock files from crashed processes.

At the campaign root, submodule ref changes (projects/*) are excluded
from staging by default to prevent accidental ref conflicts across
machines. Use --include-refs to stage them explicitly.

Use --sub to commit in the submodule detected from your current directory.
Use -p/--project to commit in a specific project (e.g., -p projects/camp).

Examples:
  camp commit -m "Add new feature"
  camp commit --amend -m "Fix typo"
  camp commit -a -m "Stage and commit all"
  camp commit --include-refs -m "Sync all submodule refs"
  camp commit --sub -m "Commit in current submodule"
  camp commit -p projects/camp -m "Commit in camp project"

```
camp commit [flags]
```

### Options

```
  -a, --all                   Stage all changes before committing (default true)
      --amend                 Amend the previous commit
      --auto-write            Run configured commit message writer
  -h, --help                  help for commit
      --include-refs          Include submodule ref changes when staging at campaign root
  -m, --message stringArray   Commit message (repeatable; multiple -m are joined git-style into subject + body; required unless --auto-write)
      --no-edit               Amend without editing the commit message (requires --amend)
  -p, --project string        Operate on a specific project/submodule path
      --sub                   Operate on the submodule detected from current directory
      --workitem string       explicit workitem selector for the commit tag (overrides cwd-based resolution)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp completion

Generate the autocompletion script for the specified shell

### Synopsis

Generate the autocompletion script for camp for the specified shell.
See each sub-command's help for details on how to use the generated script.


### Options

```
  -h, --help   help for completion
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp completion bash

Generate the autocompletion script for bash

### Synopsis

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(camp completion bash)

To load completions for every new session, execute once:

#### Linux:

	camp completion bash > /etc/bash_completion.d/camp

#### macOS:

	camp completion bash > $(brew --prefix)/etc/bash_completion.d/camp

You will need to start a new shell for this setup to take effect.


```
camp completion bash
```

### Options

```
  -h, --help              help for bash
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp completion fish

Generate the autocompletion script for fish

### Synopsis

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	camp completion fish | source

To load completions for every new session, execute once:

	camp completion fish > ~/.config/fish/completions/camp.fish

You will need to start a new shell for this setup to take effect.


```
camp completion fish [flags]
```

### Options

```
  -h, --help              help for fish
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp completion powershell

Generate the autocompletion script for powershell

### Synopsis

Generate the autocompletion script for powershell.

To load completions in your current shell session:

	camp completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.


```
camp completion powershell [flags]
```

### Options

```
  -h, --help              help for powershell
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp completion zsh

Generate the autocompletion script for zsh

### Synopsis

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(camp completion zsh)

To load completions for every new session, execute once:

#### Linux:

	camp completion zsh > "${fpath[1]}/_camp"

#### macOS:

	camp completion zsh > $(brew --prefix)/share/zsh/site-functions/_camp

You will need to start a new shell for this setup to take effect.


```
camp completion zsh [flags]
```

### Options

```
  -h, --help              help for zsh
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp concepts

List configured concepts

```
camp concepts [flags]
```

### Options

```
  -h, --help   help for concepts
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp copy

Copy a file or directory within the campaign

### Synopsis

Copy a file or directory within the current campaign.

Paths are resolved relative to the current directory, matching standard
'cp' behavior and tab completion.

Use @ prefix for campaign shortcuts (e.g., @p/fest, @f/active/).
Available shortcuts are defined in campaign config.

If the destination is an existing directory or ends with '/', the source
is placed inside it with the same basename. Directories are copied
recursively.

```
camp copy <src> <dest> [flags]
```

### Examples

```
  camp copy myfile.md ../docs/
  camp cp @f/active/my-fest/OVERVIEW.md @d/
  camp cp @w/design/active/ @w/explore/backup/
```

### Options

```
  -f, --force   Overwrite destination without prompting
  -h, --help    help for copy
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp create

Create a new campaign at the default campaigns directory

### Synopsis

Create a new campaign at <campaigns_dir>/<name>/, using the same scaffolding as 'camp init'. The default campaigns directory is ~/campaigns/ and can be configured via 'camp settings' or by editing the campaigns_dir field in ~/.obey/campaign/config.json.

```
camp create <name> [flags]
```

### Examples

```
  camp create my-project
  camp create my-project -d "Description" -m "Mission"
  camp create my-project --path ~/Dev/sandbox
  camp create my-project --dry-run
```

### Options

```
  -d, --description string   Campaign description
      --dry-run              Show what would be done without creating anything
  -h, --help                 help for create
  -m, --mission string       Campaign mission statement
  -n, --name string          Campaign display name (defaults to <name> positional)
      --no-git               Skip git repository initialization
      --no-skills            Skip linking campaign skills into .claude/skills and .agents/skills
      --org string           Assign the new campaign to this org (created if new; defaults to the fallback org)
      --path string          Override the base campaigns directory (campaign created at <path>/<name>/)
  -t, --type string          Campaign type (product, research, tools, personal) (default "product")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp date

Append date suffix to file or directory name

### Synopsis

Append a date suffix to a file or directory name.

Renames a file or directory by appending a date to its name.
Shows a preview of the rename and asks for confirmation.

Date source (in priority order):
  --mtime    Use the file's last modified date
  --ago N    Use today minus N days
  (default)  Use today's date

Examples:
  camp date old-project              # old-project -> old-project-2026-01-27
  camp date ./docs/archive.md        # archive.md -> archive-2026-01-27.md
  camp date old-project --yes        # Skip confirmation
  camp date old-project --ago 3      # Use date from 3 days ago
  camp date old-project --mtime      # Use file's last modified date
  camp date old-project -f 20060102  # Use different date format

```
camp date <path> [flags]
```

### Options

```
  -a, --ago int         Use date from N days ago
      --dry-run         Show what would be done without making changes
  -f, --format string   Date format (Go time format) (default "2006-01-02")
  -h, --help            help for date
  -m, --mtime           Use file's last modified date
  -y, --yes             Skip confirmation prompt
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp detach

Remove the attachment marker from a directory

### Synopsis

Remove the .camp attachment marker from the target directory.

Refuses on linked-project markers; use 'camp project unlink' for those.
The user-managed symlink (if any) is not modified.

Examples:
  camp detach docs/examples/external-repo
  camp detach ~/scratch/notes-link

```
camp detach <path> [flags]
```

### Options

```
  -h, --help   help for detach
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp doctor

Diagnose and fix campaign health issues

### Synopsis

Check campaign for common issues and optionally fix them.

CHECKS PERFORMED:
  orphan      Orphaned gitlinks in index (no .gitmodules entry)
  url         URL consistency between .gitmodules and .git/config
  integrity   Submodule integrity (empty/broken directories)
  head        HEAD states (detached with local work)
  working     Working directory cleanliness
  commits     Parent-submodule commit alignment
  lock        Stale git index.lock files

EXIT CODES:
  0  All checks passed (no warnings or errors)
  1  Warnings or errors found
  2  Usage error (bad flags or args)
  3  Fix attempted but some issues remain

EXAMPLES:
  # Run all checks
  camp doctor

  # Attempt automatic fixes
  camp doctor --fix

  # Run URL check only
  camp doctor -c url

  # Detailed output
  camp doctor --verbose

  # JSON output for scripting
  camp doctor --json

```
camp doctor [flags]
```

### Options

```
  -c, --check strings     Run specific check(s) only (orphan, url, integrity, head, working, commits, lock)
  -f, --fix               Attempt automatic fixes for detected issues
  -h, --help              help for doctor
      --json              Output results as JSON
      --submodules-only   Only check submodule health
  -v, --verbose           Show detailed information for each check
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp dungeon

Manage the campaign dungeon

### Synopsis

Manage the campaign dungeon - a holding area for uncertain work.

The dungeon is where you put work you're unsure about or want out of the way.
It keeps items visible without them competing for your attention.

Commands:
  add     Initialize dungeon structure with documentation
  crawl   Interactive review and archival of dungeon contents
  list    List dungeon items (agent-friendly)
  move    Move items between dungeon statuses (agent-friendly)

Examples:
  camp dungeon add                        Initialize the dungeon
  camp dungeon crawl                      Review and archive dungeon items
  camp dungeon list                       List dungeon root items
  camp dungeon list --triage              List parent items eligible for triage
  camp dungeon move old-feature archived  Move item to archived status

```
camp dungeon [flags]
```

### Options

```
  -h, --help   help for dungeon
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp dungeon add

Initialize dungeon structure

### Synopsis

Initialize the dungeon directory with documentation and structure.

Creates the dungeon directory with:
  - OBEY.md: Documentation explaining the dungeon's purpose
  - completed/: Successfully finished work
  - archived/: Preserved for history, truly done
  - someday/: Low priority, might revisit

Initialize the dungeon directory structure directly, without requiring
workflow setup (no .workflow.yaml, active/, or ready/ directories).
Useful when you only need a dungeon for idea capture or temporary holding.

This operation is idempotent - running it multiple times is safe.
Use --force to overwrite existing files.

```
camp dungeon add [flags]
```

### Examples

```
  camp dungeon add          Initialize dungeon (skip existing files)
  camp dungeon add --force  Overwrite existing documentation
```

### Options

```
  -f, --force   Overwrite existing files
  -h, --help    help for add
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp dungeon crawl

Interactive dungeon review

### Synopsis

Interactively review and archive dungeon contents.

Without flags, auto-detects what to crawl:
  - Parent items exist → triage mode (move items into dungeon)
  - Dungeon items exist → inner mode (keep/archive dungeon items)
  - Both exist → runs triage first, then inner

Use --triage or --inner to force a specific mode.

For each item, you'll be prompted to decide its fate.
Triage mode includes a route-to-docs action for existing campaign-root docs/<subdirectory>.
Statistics are gathered when available (requires scc or fest).
All decisions are logged to crawl.jsonl for history.

Examples:
  camp dungeon crawl            Auto-detect mode
  camp dungeon crawl --triage   Force triage mode only
  camp dungeon crawl --inner    Force inner mode only

```
camp dungeon crawl [flags]
```

### Options

```
  -h, --help     help for crawl
      --inner    Force inner mode (review dungeon items)
      --triage   Force triage mode (review parent items)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp dungeon list

List dungeon items

### Synopsis

List items in the dungeon or parent items eligible for triage.

By default, lists items at the dungeon root (items already in the dungeon).
Use --triage to list parent directory items that could be moved into the dungeon.
The command resolves dungeon context by walking from the current directory up to
campaign root and using the nearest available dungeon.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            Names only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp dungeon list                  List dungeon root items
  camp dungeon list --triage         List parent items eligible for triage
  cd workflow/design/subdir && camp dungeon list  Uses nearest dungeon context from nested path
  camp dungeon list --json           JSON output for scripting
  camp dungeon list -f json          JSON output for scripting
  camp dungeon list -f simple        Names only, pipe to other commands

```
camp dungeon list [flags]
```

### Options

```
  -f, --format string   Output format: table, simple, json (default "table")
  -h, --help            help for list
      --json            Output as JSON (shorthand for --format json)
      --triage          List parent items eligible for triage into dungeon
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp dungeon move

Move dungeon items between statuses

### Synopsis

Move items within the dungeon or from the parent directory into the dungeon.

By default, moves an item already in the dungeon root to a status directory.
When the item exists in the parent directory and not in the dungeon root, the
command automatically treats it as triage work and moves it into the dungeon.
Use --triage to force a parent-directory move.
With --triage and --to-docs, routes items to an existing campaign-root docs/<subdirectory>.
With --workitem, resolves a campaign workitem from anywhere and moves its directory
into the workitem type's local dungeon.
Moves are always auto-committed so dungeon history remains auditable.

Statuses: completed, archived, someday

Batch: pass several items followed by one shared status to move them together
(default, --triage, and --to-docs modes). Every item is validated before any
move is applied, so an invalid item aborts the whole sweep. --workitem accepts a
single item per invocation.

Dry run: --dry-run resolves and validates exactly as a real move would, prints
the source -> destination for each item, and exits without touching the
filesystem or creating a commit. Add --json for a machine-readable plan.

Examples:
  camp dungeon move old-feature archived         Move dungeon item to archived
  camp dungeon move stale-doc completed          Move dungeon item to completed
  camp dungeon move a b c archived               Move three items to archived (batch)
  camp dungeon move a b c completed --dry-run    Preview the sweep, change nothing
  camp dungeon move old-project --triage         Move parent item into dungeon root
  camp dungeon move old-project archived --triage Move parent item directly to archived
  camp dungeon move stale-note.md --triage --to-docs architecture/api Route to docs subdirectory
  camp dungeon move feature-slug archived --workitem Move workitem directory to its local archive

```
camp dungeon move <item>... [status] [flags]
```

### Options

```
      --dry-run          Preview the move(s) without touching the filesystem or creating a commit
  -h, --help             help for move
      --json             Emit the dry-run plan as JSON (requires --dry-run)
      --to-docs string   Route triage item into an existing campaign-root docs/<subdir> (requires --triage)
      --triage           Move from parent directory (not from dungeon root)
      --workitem         Resolve item as a campaign workitem and move its directory to the local dungeon
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp event

Record and inspect campaign ledger events

### Synopsis

Record and inspect campaign event-ledger entries.

The ledger is the append-only trail of high-intent actions across a campaign.
Most events are captured automatically by state-changing camp/fest commands;
'camp event add' is the explicit escape hatch for actions that never touch git.

### Options

```
  -h, --help   help for event
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp event add

Record an explicit campaign ledger event

### Synopsis

Record an explicit campaign ledger event for an out-of-band action.

Scope is inferred from the current directory (the workitem or festival you are
in); flags override inference. Evidence may be a campaign-relative path, a URL,
or a repo@sha commit reference, and may be repeated.

Examples:
  # A media-production decision, with the produced file as evidence
  camp event add --type decided "chose H.265 for the trailer" \
    --why "smaller files, target players all support it" \
    --evidence renders/trailer_final_v3.mp4

  # A quick note-to-trail from inside a workitem directory
  camp event add --type created "kicked off the color grade pass"

```
camp event add <title> [flags]
```

### Options

```
      --action string          join an existing action id (default: a fresh action per invocation)
      --evidence stringArray   evidence ref: <path> | <url> | <repo>@<sha> (repeatable)
      --festival string        festival id to scope the event (overrides cwd inference)
  -h, --help                   help for add
      --json                   emit a structured JSON result
      --quest string           quest id to scope the event
      --type string            event kind (required): created, transitioned, completed, decided, evidence_attached, reconciled, repaired
      --why string             the reason for the action (rendered prominently)
      --workitem string        workitem selector to scope the event (overrides cwd inference)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp festivals

List festivals across campaigns, filtered by org/tag

### Synopsis

Aggregate festivals across campaigns, filtered by campaign org/tag.

Selects campaigns from the registry by --org and --tag (AND), then composes
'fest list --json' in each matching campaign and aggregates the result. The
campaign set defaults to active campaigns; --all-campaigns includes inactive and
reference campaigns. Festival-level flags (--status, --all, --since, --until,
--sort) are passed through to each underlying 'fest list'.

Runs one 'fest list' per matching campaign (sequentially); campaigns without a
festivals/ workspace contribute nothing. Read-only.

```
camp festivals [flags]
```

### Examples

```
  camp festivals --org obey
  camp festivals --org obey --status active
  camp festivals --tag paid-work --all-campaigns --json
```

### Options

```
      --all             Include completed/dungeon festivals, passed to fest list
      --all-campaigns   Include inactive/reference campaigns (default: active only)
  -h, --help            help for festivals
  -i, --interactive     Open the interactive festivals browser
      --json            Output as JSON
      --org string      Only campaigns in this org
      --since string    Festivals created on or after this date, passed to fest list
      --sort string     Festival sort, passed to fest list
      --status string   Festival status filter, passed to fest list
      --tag strings     Only campaigns carrying this tag (repeat for AND)
      --until string    Festivals created on or before this date, passed to fest list
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp fresh

Post-merge branch cycling: sync to default branch and optionally create a new working branch

### Synopsis

Reset one or more projects to a fresh state after merging a PR.

Performs the post-merge cycle: checkout default branch, pull latest,
prune merged branches, and optionally create a new working branch.

Auto-detects the current project from your working directory, or accepts a
single project name. Use --list to cycle a specific set of projects in one
run, or 'camp fresh all' to cycle every project submodule in the campaign.

Without configuration, syncs to the default branch and prunes.
Configure .campaign/settings/fresh.yaml to set a default working branch, or
follow-up command workflows (install, build, bootstrap, ...) to run once the
cycle succeeds. Manage those with 'camp fresh configure'. Inspect the resolved
sequence with 'camp fresh show-workflow [project-name]'.

Examples:
  camp fresh                            # Sync current project (checkout default, pull, prune)
  camp fresh --branch develop           # Sync and create develop branch
  camp fresh camp -b feat/new-thing     # Sync camp project, create feature branch
  camp fresh --list camp,fest,festival  # Sync a specific set of projects
  camp fresh --no-prune                 # Sync without pruning
  camp fresh --no-follow-up             # Sync without running configured follow-ups
  camp fresh --dry-run                  # Preview what would happen (follow-ups listed, not run)

```
camp fresh [project-name] [flags]
```

### Options

```
  -b, --branch string    Branch to create after syncing (overrides config)
  -n, --dry-run          Preview without making changes
  -h, --help             help for fresh
      --list strings     Comma-separated set of projects to cycle in one run
      --no-branch        Skip branch creation even if configured
      --no-follow-up     Skip configured follow-up command workflows
      --no-prune         Skip pruning merged branches
      --no-push          Skip pushing the new branch upstream
  -p, --project string   Project name (auto-detected from cwd)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp fresh all

Run fresh across all project submodules

### Synopsis

Run the fresh cycle (checkout default, pull, prune, optional branch)
across every project submodule in the campaign.

Examples:
  camp fresh all                     # Sync all projects
  camp fresh all --branch develop    # Sync all and create develop branch
  camp fresh all --dry-run           # Preview across all projects
  camp fresh all --no-prune          # Sync without pruning

```
camp fresh all [flags]
```

### Options

```
  -h, --help   help for all
```

### Options inherited from parent commands

```
  -b, --branch string   Branch to create after syncing (overrides config)
  -n, --dry-run         Preview without making changes
      --no-branch       Skip branch creation even if configured
      --no-color        disable colored output
      --no-follow-up    Skip configured follow-up command workflows
      --no-prune        Skip pruning merged branches
      --no-push         Skip pushing the new branch upstream
```
---

## camp fresh configure

Configure camp fresh follow-up commands

### Synopsis

Manage the follow-up command workflows camp fresh runs after a
successful sync/prune/branch cycle. Configuration lives in
.campaign/settings/fresh.yaml: a global default list, plus optional
per-project override lists that replace the global list entirely.

Run without a subcommand to open the interactive setup for humans. Use
show, add, move, and remove for scripts and agents.

Examples:
  camp fresh configure
  camp fresh show-workflow camp
  camp fresh configure show
  camp fresh configure add install --run "npm install"
  camp fresh configure add build --run "go build ./..." --project camp --dir cmd/camp
  camp fresh configure move build --up --project camp
  camp fresh configure remove install
  camp fresh configure remove build --project camp

```
camp fresh configure [flags]
```

### Options

```
  -h, --help   help for configure
```

### Options inherited from parent commands

```
  -b, --branch string   Branch to create after syncing (overrides config)
  -n, --dry-run         Preview without making changes
      --no-branch       Skip branch creation even if configured
      --no-color        disable colored output
      --no-follow-up    Skip configured follow-up command workflows
      --no-prune        Skip pruning merged branches
      --no-push         Skip pushing the new branch upstream
```
---

## camp fresh configure add

Add a follow-up command workflow step

```
camp fresh configure add <name> [flags]
```

### Options

```
      --continue-on-error   Keep running later follow-ups if this step fails
      --dir string          Directory relative to the project root to run the command in
  -h, --help                help for add
      --project string      Scope this follow-up to a single project (default: global)
      --run string          Command to run for this follow-up step (required)
```

### Options inherited from parent commands

```
  -b, --branch string   Branch to create after syncing (overrides config)
  -n, --dry-run         Preview without making changes
      --no-branch       Skip branch creation even if configured
      --no-color        disable colored output
      --no-follow-up    Skip configured follow-up command workflows
      --no-prune        Skip pruning merged branches
      --no-push         Skip pushing the new branch upstream
```
---

## camp fresh configure move

Move a follow-up command workflow step

```
camp fresh configure move <name> [flags]
```

### Options

```
      --down             Move the step later in the workflow
  -h, --help             help for move
      --project string   Scope the move to a single project (default: global)
      --up               Move the step earlier in the workflow
```

### Options inherited from parent commands

```
  -b, --branch string   Branch to create after syncing (overrides config)
  -n, --dry-run         Preview without making changes
      --no-branch       Skip branch creation even if configured
      --no-color        disable colored output
      --no-follow-up    Skip configured follow-up command workflows
      --no-prune        Skip pruning merged branches
      --no-push         Skip pushing the new branch upstream
```
---

## camp fresh configure remove

Remove a follow-up command workflow step

```
camp fresh configure remove <name> [flags]
```

### Options

```
  -h, --help             help for remove
      --project string   Scope removal to a single project (default: global)
```

### Options inherited from parent commands

```
  -b, --branch string   Branch to create after syncing (overrides config)
  -n, --dry-run         Preview without making changes
      --no-branch       Skip branch creation even if configured
      --no-color        disable colored output
      --no-follow-up    Skip configured follow-up command workflows
      --no-prune        Skip pruning merged branches
      --no-push         Skip pushing the new branch upstream
```
---

## camp fresh configure show

Show configured follow-up workflows

```
camp fresh configure show [flags]
```

### Options

```
  -h, --help   help for show
```

### Options inherited from parent commands

```
  -b, --branch string   Branch to create after syncing (overrides config)
  -n, --dry-run         Preview without making changes
      --no-branch       Skip branch creation even if configured
      --no-color        disable colored output
      --no-follow-up    Skip configured follow-up command workflows
      --no-prune        Skip pruning merged branches
      --no-push         Skip pushing the new branch upstream
```
---

## camp fresh show-workflow

Show the fresh cycle and configured follow-up steps

### Synopsis

Show the ordered steps camp fresh will use, including disabled steps
and the follow-up commands resolved for a project.

With no project name, the global defaults are shown. Pass a project name to
include its branch, pruning, and follow-up overrides.

```
camp fresh show-workflow [project-name] [flags]
```

### Options

```
  -h, --help   help for show-workflow
```

### Options inherited from parent commands

```
  -b, --branch string   Branch to create after syncing (overrides config)
  -n, --dry-run         Preview without making changes
      --no-branch       Skip branch creation even if configured
      --no-color        disable colored output
      --no-follow-up    Skip configured follow-up command workflows
      --no-prune        Skip pruning merged branches
      --no-push         Skip pushing the new branch upstream
```
---

## camp gather

Gather related work into unified items

### Synopsis

Gather related work into unified items.

Available sources:
  feedback    Import feedback observations from festivals into intents
  design      Combine selected design workitems into one gathered package

For gathering intents by tag, hashtag, or similarity, see 'camp intent gather'.

```
camp gather [flags]
```

### Options

```
  -h, --help   help for gather
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp gather design

Combine selected design workitems into one gathered package

### Synopsis

Combine selected design workitems into one gathered package.

Sources are always chosen explicitly: pass 2 or more selectors (stable id,
key, path, or directory slug), or run with no arguments in a terminal for an
interactive picker. There is no automatic discovery mode.

The gather process:
  1. Create workflow/design/<slug>/ with a fresh .workitem and a
     generated README.md indexing the gathered packages
  2. Move each source directory inside the new package (git history follows
     the rename)
  3. Stamp gathered_into/gathered_at on each source .workitem
  4. Migrate manual priority state and re-home workitem links
  5. Commit the move (unless --no-commit)

Moved sources stop appearing as separate workitems because discovery only
scans the top level of workflow/design/.

Examples:
  camp gather design pkg-one pkg-two --title "Unified topic"
  camp gather design pkg-one pkg-two pkg-three -t "Unified topic" --slug unified-topic
  camp gather design                # interactive picker (TTY only)
  camp gather design pkg-one pkg-two -t "Unified topic" --dry-run

```
camp gather design [selectors...] [flags]
```

### Options

```
      --dry-run        Print the planned gather, change nothing
      --force          Gather sources even when one has an active workflow run
  -h, --help           help for design
      --json           Output result as a single JSON object
      --no-commit      Skip the auto-commit
      --slug string    Directory slug override (default: derived from title)
  -t, --title string   Title for the gathered workitem (required unless prompted interactively)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp gather feedback

Gather feedback observations from festivals into intents

### Synopsis

Scan festival directories for feedback observations and create
trackable FEEDBACK intent files with checkboxes.

Each festival with feedback observations gets a FEEDBACK_<fest_id>.md intent
in .campaign/intents/inbox/. Observations are grouped by criteria with
checkboxes for tracking addressed status.

Deduplication tracking ensures observations are only gathered once.
Re-running the command appends only new observations to existing intents,
preserving any checkbox state from previous runs.

Examples:
  # Gather all feedback from all festivals
  camp gather feedback

  # Preview what would be gathered
  camp gather feedback --dry-run

  # Gather from a specific festival
  camp gather feedback --festival-id CC0004

  # Only gather from completed festivals
  camp gather feedback --status completed

  # Filter by severity
  camp gather feedback --severity high

  # Re-gather everything (ignore tracking)
  camp gather feedback --force

```
camp gather feedback [flags]
```

### Options

```
      --dry-run              Preview without creating intents
      --festival-id string   Only gather from a specific festival
      --force                Re-gather all, ignoring tracking
  -h, --help                 help for feedback
      --no-commit            Skip git commit
      --severity string      Filter by observation severity (low, medium, high)
      --status string        Festival status dirs to scan (comma-separated) (default "completed,active,planned")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp go

Navigate to campaign directories

### Synopsis

Navigate within the campaign using shortcuts.

Usage patterns:
  camp go           Toggle between campaign root and last location
  camp go --root    Jump to campaign root (ignore toggle)
  camp go t         Jump to last visited location (cd - equivalent)
  camp go p         Jump to projects/
  camp go f         Jump to festivals/
  camp go design    Jump to an exact pin name
  camp go p api     Fuzzy search projects/ for "api"

Toggle behavior (no args):
  - From anywhere: jump to campaign root, save current location
  - From campaign root: jump back to saved location

Toggle keyword (t / toggle):
  - Jump to the last visited location regardless of where you are
  - Repeated calls alternate between two locations (like cd -)

Pins:
  - Create a named pin with 'camp pin <name> [path]'
  - Jump to an exact pin with 'camp go <name>' or 'cgo <name>'
  - Pin jumps save your current location first, so 'camp go t' or 'cgo t'
    can bounce back to where you came from

The --print flag outputs just the path for shell integration:
  cd "$(camp go p --print)"

The -c flag runs a command from the directory without changing to it:
  camp go p -c ls           List contents of projects/
  camp go f -c fest status  Run fest status from festivals/

Or use the cgo shell function for instant navigation:
  cgo               Toggle between root and last location
  cgo p             Equivalent to: cd "$(camp go p --print)"
  cgo p -c ls       Run ls in projects/ without changing directory

```
camp go [shortcut] [query...] [flags]
```

### Examples

```
  camp go               # Toggle: root ↔ last location
  camp go --root        # Force jump to campaign root
  camp go t             # Jump to last visited location (cd -)
  camp go p             # Jump to projects/
  camp go design        # Jump to exact pin "design"
  camp go p api         # Fuzzy find "api" in projects/
  camp go p --print     # Print path (for shell scripts)
  cgo design            # Shell jump to exact pin "design"
  cgo t                 # Jump back after a pin jump
  camp go f -c ls       # List festivals/ without cd
```

### Options

```
  -c, --command stringArray   Run command from directory (can be repeated for args)
  -h, --help                  help for go
  -l, --list                  List available sub-shortcuts for a project
      --print                 Print path only (for shell integration)
      --root                  Jump to campaign root (ignore last location)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp id

Print the current campaign ID

### Synopsis

Print the current campaign ID from .campaign/campaign.yaml.

```
camp id [flags]
```

### Examples

```
  camp id
```

### Options

```
  -h, --help   help for id
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp init

Initialize a new campaign

### Synopsis

Initialize a new campaign directory structure.

Creates the standard campaign directories:
  .campaign/              - Campaign configuration and metadata
  .campaign/intents/      - System-managed intent state
  projects/               - Project repositories (submodules or worktrees)
  projects/worktrees/     - Git worktrees for parallel development
  festivals/              - Festival methodology workspace (via fest init)
  docs/                   - Human-authored documentation
  dungeon/                - Archived and deprioritized work
  workflow/               - Workflow management
  workflow/reviews/       - Review notes, feedback, and assessments
  workflow/design/        - Design documents

Also creates:
  AGENTS.md     - AI agent instruction file
  CLAUDE.md     - Symlink to AGENTS.md

Initializes a git repository if not already inside one.

Use --no-git to skip git initialization.

```
camp init [path] [flags]
```

### Examples

```
  camp init                      Initialize current directory
  camp init my-campaign          Create and initialize new directory
  camp init --name "My Project"  Set custom campaign name
  camp init --no-git             Skip git initialization
  camp init --dry-run            Preview without creating anything
```

### Options

```
  -d, --description string   Campaign description
      --dry-run              Show what would be done without creating anything
  -f, --force                Initialize in non-empty directory without prompting
  -h, --help                 help for init
  -m, --mission string       Campaign mission statement
  -n, --name string          Campaign name (defaults to directory name)
      --no-git               Skip git repository initialization
      --no-register          Don't add to global registry
      --no-skills            Skip linking campaign skills into .claude/skills and .agents/skills
      --org string           Assign the new campaign to this org (created if new; defaults to the fallback org)
      --repair               Add missing files to existing campaign
  -t, --type string          Campaign type (product, research, tools, personal) (default "product")
  -v, --verbose              Show skipped optional setup details
      --yes                  Skip repair confirmation prompt (for scripting)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent

Manage campaign intents

### Synopsis

Manage intents for ideas and features not yet ready for full planning.

Intents capture ideas, bugs, features, and research topics that depend on work
not yet completed. They serve as structured storage for ideas that aren't ready
to become Festivals but need to be tracked.

CAPTURE MODES:
  Fast (default)    Quick capture with minimal fields
  Deep (--edit)     Open in editor for full context

INTENT LIFECYCLE:
  inbox  → Captured, not yet reviewed
  ready  → Reviewed/enriched, ready for promotion
  active → Promoted to festival/design doc, work in progress
  dungeon/* → Terminal statuses (done, killed, archived, someday)

Examples:
  camp intent add "Add dark mode toggle"         Fast capture to inbox
  camp intent add -e "Refactor auth system"      Deep capture with editor
  camp intent list                               List all intents
  camp intent list --status active               List active intents
  camp intent edit add-dark                      Edit intent (fuzzy match)
  camp intent show 20260119-153412-add-dark      Show intent details
  camp intent move add-dark ready                Mark as ready
  camp intent promote add-dark                   Promote to active via festival
  camp intent archive add-dark                   Archive intent

```
camp intent [flags]
```

### Options

```
  -h, --help   help for intent
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent add

Create a new intent

### Synopsis

Create a new intent with fast or deep capture mode.

CAPTURE MODES:
  Ultra-fast          Title provided as argument → immediate creation
  Fast TUI (default)  Step-through form (title, type, concept)
  Full TUI (--full)   Step-through form including body textarea
  Deep (--edit)       Full template in $EDITOR

Fast capture is optimized for speed - ideas are saved immediately.
Use --full when you want to add a body description in the form.
Use --edit when you need the complete template in your editor.

PROGRAMMATIC (agent) FLAGS:
  --body              Set intent body from a literal string
  --body-file         Read intent body from a file (- for stdin)
  --concept           Set the concept field (e.g., "projects/camp")
  --note              Create a note instead of a lifecycle intent
  --author            Override the default author attribution

  --body and --body-file are mutually exclusive.
  --full + body flags is a usage error.
  --edit + body flags pre-fills the editor template.

Examples:
  camp intent add "Add dark mode"        Ultra-fast capture
  camp intent add -c obey-campaign "Add dark mode"
  camp intent add                        Fast TUI (3-step form)
  camp intent add --campaign             Pick a target campaign interactively
  camp intent add --full                 Full TUI (includes body)
  camp intent add --note                 Note TUI (title + body, no type/concept)
  camp intent add --note "Meeting note" --body "Follow up next week"
  camp intent add -e "Complex feature"   Deep capture with editor
  camp intent add -t feature "New API"   Set type explicitly
  camp intent add "Fix login" --body "The login page returns 500"
  camp intent add "Migrate DB" --body-file spec.md --concept projects/camp
  echo "body" | camp intent add "Idea" --body-file -

```
camp intent add [title] [flags]
```

### Options

```
      --author string      Override the default author attribution
      --body string        Set intent body as a literal string
      --body-file string   Read intent body from file (- for stdin, 10 MiB cap)
  -c, --campaign string    Target campaign by name or ID; omit value to pick interactively
      --concept string     Set the concept field (e.g., projects/camp)
  -e, --edit               Open in $EDITOR for deep capture
      --full               Full TUI mode with body textarea
  -h, --help               help for add
      --json               emit a structured JSON result
      --no-commit          Don't create a git commit
      --note               Create a note instead of a lifecycle intent
      --tag stringArray    Add a tag (repeatable)
  -t, --type string        Intent type (idea, feature, bug, research, chore) (default "idea")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent archive

Archive an intent

### Synopsis

Archive an intent by moving it to dungeon/archived.

This is a convenience command equivalent to:
  camp intent move <id> archived --reason "..."

Dungeon moves require a reason and append a decision record to the intent body.
Use 'camp intent move <id> inbox' to un-archive if needed.

Examples:
  camp intent archive add-dark --reason "superseded by broader initiative"
  camp intent archive 20260119-153412 --reason "preserve as reference"

```
camp intent archive <id> [flags]
```

### Options

```
  -h, --help            help for archive
      --no-commit       Don't create a git commit
      --reason string   Reason for archiving (required)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent convert

Convert a note into an intent

### Synopsis

Promote a note into the intent lifecycle.

A note lives outside the inbox → ready → active lifecycle. Converting it moves
the note into inbox/ and attaches an intent type, after which it behaves like
any other intent. This is the only bridge from a note into the lifecycle.

Examples:
  camp intent convert check-daemon-socket --type idea
  camp intent convert check-daemon-socket -t feature

```
camp intent convert <id> [flags]
```

### Options

```
  -h, --help          help for convert
      --no-commit     Don't create a git commit
  -t, --type string   Intent type to attach (idea, feature, bug, research, chore) (default "idea")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent count

Count intents by status directory

### Synopsis

Display a count of intents grouped by status directory.

OUTPUT FORMATS:
  table (default)   Styled summary with counts per status
  json              Machine-readable JSON output

Examples:
  camp intent count              Show counts per status
  camp intent count -f json      JSON output for scripting

```
camp intent count [flags]
```

### Options

```
  -f, --format string   Output format: table, json (default "table")
  -h, --help            help for count
      --json            emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent crawl

Interactive intent triage

### Synopsis

Walk live intents one at a time and decide their fate.

Default scope is the working set: inbox, ready, and active. Each candidate is
shown with a compact preview. For each one you can keep, move to another
status, skip, or quit. Moves to dungeon statuses require a reason.

Existing dungeon intents are not crawl candidates. Use 'camp intent move' to
restore them explicitly.

Examples:
  camp intent crawl
  camp intent crawl --status inbox --limit 25
  camp intent crawl --status ready --status active --sort priority
  camp intent crawl --no-commit

```
camp intent crawl [flags]
```

### Options

```
  -h, --help             help for crawl
      --limit int        Stop after N candidates (0 = no limit)
      --no-commit        Apply moves and logs but do not auto-commit
      --sort string      Sort mode: stale, updated, created, priority, title (default "stale")
      --status strings   Restrict to live statuses (repeatable: inbox, ready, active)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent edit

Edit an existing intent

### Synopsis

Edit an intent in your preferred editor or programmatically via flags.

If no programmatic flags are given, opens the intent in $EDITOR.
If any programmatic flag is present, applies the update directly and
emits an audit event — no editor is launched.

PICKER / EDITOR PATH:
  If ID is provided, opens the intent directly (supports partial matching).
  If no ID is provided, shows a fuzzy picker to select an intent.

PROGRAMMATIC FLAGS (skip $EDITOR):
  --title            Set a new title
  --body             Replace the body with a literal string
  --body-file        Replace the body from a file (- for stdin)
  --append-body      Append text to the existing body
  --append-body-file Append text from a file (- for stdin)
  --set-type         Change the intent type
  --set-status       Change the intent status
  --set-concept      Change the concept field
  --priority         Change priority (low, medium, high)
  --horizon          Change horizon (now, next, later, someday)
  --author           Override the author attribution

MUTUAL EXCLUSIVITY:
  --body vs --body-file
  --append-body vs --append-body-file
  --body/--body-file vs --append-body/--append-body-file (replace vs append)

FILTER FLAGS (for picker only, not update targets):
  -s/--status        Filter picker by status
  -t/--type          Filter picker by type
  -p/--project       Filter picker by project/concept

Examples:
  camp intent edit                                Interactive picker + $EDITOR
  camp intent edit retry-logic                    Direct edit by partial ID
  camp intent edit --status active                Picker filtered by status
  camp intent edit retry --title "Retry with backoff"
  camp intent edit retry --body "New description"
  camp intent edit retry --append-body "Additional note"
  camp intent edit retry --set-type feature --priority high
  echo "details" | camp intent edit retry --body-file -

```
camp intent edit [id] [flags]
```

### Options

```
      --append-body string        Append text to the existing body
      --append-body-file string   Append text from file (- for stdin, 10 MiB cap)
      --author string             Override the author attribution
      --body string               Replace the intent body
      --body-file string          Replace body from file (- for stdin, 10 MiB cap)
  -h, --help                      help for edit
      --horizon string            Change horizon (now, next, later, someday)
      --no-commit                 Don't create a git commit
      --priority string           Change priority (low, medium, high)
  -p, --project string            Filter picker by project
      --set-concept string        Change the concept field
      --set-status string         Change the intent status
      --set-type string           Change the intent type (idea, feature, bug, research, chore)
  -s, --status string             Filter picker by status
      --tag stringArray           Replace the intent's tags (repeatable)
      --title string              Set a new title
  -t, --type string               Filter picker by type
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent explore

Interactive intent explorer

### Synopsis

Launch the interactive Intent Explorer TUI.

The explorer provides a full-screen interface for browsing,
filtering, and managing intents with keyboard shortcuts.

NAVIGATION
  j/↓           Move down
  k/↑           Move up
  g             Go to top (preview)
  G             Go to bottom (preview)
  Enter/Space   Select/expand group
  Tab           Switch focus (list/preview)

ACTIONS
  e             Edit in $EDITOR
  o             Open with system handler
  O             Reveal in file manager
  n             New intent
  p             Promote to next status
  a             Archive intent
  d             Delete intent
  m             Move intent to status

GATHER (Multi-Select)
  Space         Toggle selection / enter gather mode
  ga            Gather selected intents
  Escape        Exit multi-select mode

FILTERS
  /             Search intents (fuzzy)
  t             Filter by type
  s             Filter by status
  c             Filter by concept
  C             Clear concept filter
  Escape        Clear filter/cancel

VIEW
  v             Toggle preview pane
  ?             Show help overlay
  q             Quit explorer

Examples:
  camp intent explore          Launch the intent explorer

```
camp intent explore [flags]
```

### Options

```
  -h, --help   help for explore
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent find

Search for intents by title or content

### Synopsis

Search for intents across all statuses by title, content, or ID.

The search is case-insensitive and matches partial strings.
Without a query, returns all intents.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            IDs only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp intent find                   List all intents
  camp intent find dark              Find intents containing "dark"
  camp intent find "bug fix"         Find intents with "bug fix"
  camp intent find -f simple auth    Get IDs of auth-related intents

```
camp intent find [query] [flags]
```

### Options

```
  -f, --format string   Output format: table, simple, json (default "table")
  -h, --help            help for find
      --json            emit a structured JSON result
  -n, --limit int       Limit results (0 = no limit)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent gather

Gather related intents into a unified document

### Synopsis

Gather multiple related intents into a single unified document.

DISCOVERY MODES:
  By IDs      Explicitly specify intent IDs to gather
  --tag       Find intents with a specific frontmatter tag
  --hashtag   Find intents containing a specific #hashtag
  --similar   Find intents similar to a given ID (TF-IDF)

The gather process:
  1. Find related intents using the specified discovery method
  2. Merge their content with full metadata preservation
  3. Create a new unified intent in inbox status
  4. Archive source intents (unless --no-archive)

Source intents are preserved with a 'gathered_into' reference.

Examples:
  # Gather by explicit IDs
  camp intent gather id1 id2 id3 --title "Auth System"

  # Find and gather by tag
  camp intent gather --tag auth --title "Auth System"

  # Find and gather by hashtag
  camp intent gather --hashtag login --title "Login System"

  # Find similar intents and gather
  camp intent gather --similar auth-feature --title "Auth Unified"

  # Gather without archiving sources
  camp intent gather id1 id2 --title "Combined" --no-archive

  # Dry run to preview what would be gathered
  camp intent gather --tag auth --title "Auth System" --dry-run

```
camp intent gather [ids...] [flags]
```

### Options

```
      --concept string    Override concept path
      --dry-run           Preview gather without making changes
      --hashtag string    Find intents by content hashtag
  -h, --help              help for gather
      --horizon string    Override horizon (now, next, later, someday)
      --min-score float   Minimum similarity score (0.0-1.0) (default 0.1)
      --no-archive        Don't archive source intents
      --no-commit         Don't create a git commit
      --priority string   Override priority (low, medium, high)
      --similar string    Find intents similar to this ID
      --tag string        Find intents by frontmatter tag
  -t, --title string      Title for the gathered intent (required)
      --type string       Override type (idea, feature, bug, research)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent list

List intents in the campaign

### Synopsis

List intents with filtering, sorting, and output format options.

By default, lists intents in inbox, active, and ready status.
Use --all to include dungeon intents.

OUTPUT FORMATS:
  table (default)   Human-readable table with columns
  simple            IDs only, one per line (for scripting)
  json              Full metadata in JSON format

Examples:
  camp intent list                         List active intents
  camp intent ls --status inbox            List inbox only
  camp intent list -f json                 JSON output
  camp intent list -f simple | xargs ...   Pipe IDs to commands
  camp intent list --all                   Include archived

```
camp intent list [flags]
```

### Options

```
  -a, --all              Include dungeon intents
  -f, --format string    Output format: table, simple, json (default "table")
  -h, --help             help for list
      --horizon string   Filter by horizon
      --json             emit a structured JSON result
  -n, --limit int        Limit results (0 = no limit)
  -p, --project string   Filter by project
  -S, --sort string      Sort by: updated, created, priority, title (default "updated")
  -s, --status strings   Filter by status (repeatable)
  -t, --type strings     Filter by type (repeatable)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent move

Move intent to a different status

### Synopsis

Transition an intent between lifecycle statuses.

VALID STATUSES:
  inbox      Captured, not yet reviewed
  ready      Reviewed/enriched, ready to be promoted
  active     Promoted to festival/design, work in progress
  done       Resolved (dungeon)
  killed     Abandoned (dungeon)
  archived   Preserved but inactive (dungeon)
  someday    Deferred (dungeon)

PIPELINE ORDER:
  inbox → ready → active → dungeon/done

Move is an escape hatch that allows any-to-any transitions.
Dungeon moves require a --reason flag.
You can use short dungeon names (done) or canonical paths (dungeon/done).

Examples:
  camp intent move add-dark ready                         Mark as ready
  camp intent move add-dark done --reason "completed"     Mark as done
  camp intent move add-dark killed --reason "superseded"  Kill intent

```
camp intent move <id> <status> [flags]
```

### Options

```
  -h, --help            help for move
      --no-commit       Don't create a git commit
      --reason string   Reason for the move (required for dungeon targets)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent note

Capture a quick note

### Synopsis

Capture a freeform note. Notes are a separate category from intents: they
are stored in .campaign/intents/notes/ and do not flow through the
inbox → ready → active lifecycle. A note carries no type or concept; tags
organize them.

Fast capture skips the TUI. Interactive capture uses the same title/body/tag
flow as intent add, but skips the type wheel and concept picker.

Examples:
  camp intent note "check the daemon socket path"   Capture a note immediately
  camp intent note "follow up" --body "details..."  Note with a longer body
  echo "body" | camp intent note "idea" --body-file -
  camp intent note                                  Note TUI (title + body)

```
camp intent note [text] [flags]
```

### Options

```
      --author string      Override the default author attribution
      --body string        Set note body as a literal string
      --body-file string   Read note body from file (- for stdin, 10 MiB cap)
  -h, --help               help for note
      --no-commit          Don't create a git commit
  -t, --tag stringArray    Add a tag (repeatable)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent promote

Promote an intent through the pipeline

### Synopsis

Promote an intent to the next pipeline stage.

TARGETS:
  ready      Move from inbox to ready (reviewed/enriched)
  festival   Move from ready to active + create festival (default)
  design     Move from ready to active + create design doc

The intent moves to active status when promoted to festival or design,
because work is just beginning. Use --force to bypass status checks.

Examples:
  camp intent promote add-dark                       Promote ready → festival
  camp intent promote add-dark --target design       Promote ready → design doc
  camp intent promote add-dark --target ready         Promote inbox → ready
  camp intent promote add-dark --force                Force promote from any status

```
camp intent promote <id> [flags]
```

### Options

```
      --dry-run         Preview promotion without making changes
      --force           Promote even if not in expected status
  -h, --help            help for promote
      --no-commit       Don't create a git commit
      --target string   Promote target: ready, festival, design (default "festival")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent rename

Rename an intent

### Synopsis

Rename an intent: update its title and regenerate its human-readable
filename. The intent's stable id is preserved, so references and lookups survive
the rename.

Resolution is by exact id (run 'camp intent list' to copy one).

Examples:
  camp intent rename add-dark-mode-20260119-153412 "Add a dark mode toggle"

```
camp intent rename <id> <new title> [flags]
```

### Options

```
  -h, --help        help for rename
      --no-commit   Don't create a git commit
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp intent show

Show detailed intent information

### Synopsis

Display detailed information about a specific intent.

Supports partial ID matching - you can use:
  - Full ID: 20260119-153412-add-retry-logic
  - Time suffix: 153412-add-retry
  - Slug portion: add-retry

OUTPUT FORMATS:
  text (default)   Human-readable detailed view
  json             Full metadata in JSON format
  yaml             Full metadata in YAML format

Examples:
  camp intent show 20260119-153412...    Show by full ID
  camp intent show retry-logic           Show by partial match
  camp intent show retry -f json         JSON output
  camp intent show retry -f yaml         YAML output

```
camp intent show <id> [flags]
```

### Options

```
  -f, --format string   Output format: text, json, yaml (default "text")
  -h, --help            help for show
      --json            emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp leverage

Compute leverage scores for campaign projects

### Synopsis

Compute productivity leverage scores by comparing scc COCOMO estimates
against actual development effort.

Leverage score measures how much more output you produce versus what
traditional estimation models predict for the same team and time.

  FullLeverage   = (EstimatedPeople x EstimatedMonths) / (ActualPeople x ElapsedMonths)
  SimpleLeverage = EstimatedPeople / ActualPeople

Examples:
  camp leverage                              Show team leverage (auto-detect authors from git)
  camp leverage --author lance@example.com   Show personal leverage
  camp leverage --project camp               Show score for specific project
  camp leverage --json                       Output as JSON
  camp leverage --people 2                   Override team size
  camp leverage --verbose                    Show diagnostic details
  camp leverage .                            Score current directory only
  camp leverage --dir /path/to/repo          Score a specific directory

```
camp leverage [directory] [flags]
```

### Options

```
      --author string    filter by author email (git substring match — 'alice@co' matches 'alice@co.com')
      --by-author        show per-author leverage breakdown
      --dir string       score a specific directory (skips campaign project resolution)
  -h, --help             help for leverage
      --json             output as JSON
      --no-legend        hide the leverage formula legend
      --people int       override team size (0 = auto-detect from git)
  -p, --project string   filter by project name
  -v, --verbose          show diagnostic details (config, project resolution, exclusions)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp leverage backfill

Reconstruct historical leverage data from git history

### Synopsis

Backfill analyzes past commits to build leverage-over-time data.

Uses git worktrees to check out weekly snapshots, run scc analysis,
and compute leverage scores at each point in time. Results are stored
as snapshots for later retrieval via 'camp leverage history'.

Backfill is incremental: re-running only processes dates without
existing snapshots.

Examples:
  camp leverage backfill                       Backfill all projects
  camp leverage backfill --project camp        Backfill specific project
  camp leverage backfill --workers 2           Limit concurrency
  camp leverage backfill --since 2025-06-01    Backfill from June 2025

```
camp leverage backfill [flags]
```

### Options

```
  -h, --help             help for backfill
  -p, --project string   backfill a single project
      --since string     start date (YYYY-MM-DD), overrides config project_start
  -w, --workers int      number of parallel workers (default 4)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp leverage config

View or update leverage configuration

### Synopsis

View or update leverage score configuration settings.

Without flags, displays the current configuration. With flags, updates
the configuration and saves it to .campaign/leverage/config.json.

Configuration parameters:
  --people       Number of developers on the team
  --start        Project start date (YYYY-MM-DD format)
  --cocomo-type  COCOMO project type (organic, semi-detached, embedded)
  --exclude      Exclude a project from leverage scoring
  --include      Include a previously excluded project

Examples:
  camp leverage config                         Show current config
  camp leverage config --people 3              Set team size to 3
  camp leverage config --start 2025-01-01      Set project start date
  camp leverage config --exclude obey-daemon   Exclude a project
  camp leverage config --include obey-daemon   Re-include a project

```
camp leverage config [flags]
```

### Options

```
      --author-email string   default author email for personal leverage (empty = team view)
      --cocomo-type string    COCOMO project type (organic, semi-detached, embedded)
      --exclude string        exclude a project from leverage scoring
  -h, --help                  help for config
      --include string        include a previously excluded project
      --people int            number of developers on the team (0 = auto-detect from git)
      --start string          project start date (YYYY-MM-DD)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp leverage history

Show leverage score history over time

### Synopsis

Display leverage data aggregated over time from stored snapshots.

Shows how leverage has changed week by week. Use --by-author to see
per-contributor leverage breakdown based on git blame attribution.

Requires snapshot data from 'camp leverage backfill' or 'camp leverage snapshot'.

Examples:
  camp leverage history                            Show all history
  camp leverage history --project camp             Filter to one project
  camp leverage history --since 2025-06-01         Start from June 2025
  camp leverage history --json                     Output as JSON
  camp leverage history --by-author                Per-author breakdown

```
camp leverage history [flags]
```

### Options

```
      --by-author        show per-author leverage breakdown
  -h, --help             help for history
      --json             output as JSON
      --period string    aggregation period: weekly or monthly (default "monthly")
  -p, --project string   filter to specific project
      --since string     start date (YYYY-MM-DD)
      --until string     end date (YYYY-MM-DD, default: today)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp leverage reset

Clear all cached leverage data to allow full recomputation

### Synopsis

Reset deletes cached snapshots and blame data so that leverage can
recompute from scratch.

Without flags, all project caches are removed. Use --project to clear
only a single project's data.

Examples:
  camp leverage reset                    Clear all cached data
  camp leverage reset --project camp     Clear only camp's cached data

```
camp leverage reset [flags]
```

### Options

```
  -h, --help             help for reset
  -p, --project string   clear snapshots for a single project
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp leverage snapshot

Capture current leverage state as a snapshot

### Synopsis

Capture the current leverage state for all projects (or a specific project)
and save as JSON snapshots for historical tracking.

Each snapshot includes scc metrics, computed leverage scores, and per-author
LOC attribution from git blame.

Snapshots are stored in .campaign/leverage/snapshots/<project>/<date>.json.
Re-running on the same date overwrites the previous snapshot.

Examples:
  camp leverage snapshot                  Snapshot all projects
  camp leverage snapshot --project camp   Snapshot specific project

```
camp leverage snapshot [flags]
```

### Options

```
  -h, --help             help for snapshot
  -p, --project string   snapshot a specific project only
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp lifecycle

Manage campaign lifecycle status

### Synopsis

Manage a campaign's lifecycle status.

The status is one of a fixed set:
  active      in current use (default); shown in 'camp list'
  inactive    paused or shelved; hidden from default 'camp list'
  reference   preserved read-only context; hidden from default views

Setting inactive or reference does not unregister the campaign; use
'camp unregister' to remove it from the registry entirely.

This group is 'camp lifecycle', not 'camp status' ('camp status' is the git
status wrapper).

```
camp lifecycle [flags]
```

### Examples

```
  camp lifecycle set old-project reference
  camp lifecycle list
```

### Options

```
  -h, --help   help for lifecycle
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp lifecycle list

List status counts across the registry

```
camp lifecycle list [flags]
```

### Examples

```
  camp lifecycle list
```

### Options

```
  -h, --help   help for list
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp lifecycle set

Set a campaign's lifecycle status

### Synopsis

Transition a campaign to one of: active, inactive, reference.

Any other value is rejected. Setting inactive or reference does not unregister
the campaign.

```
camp lifecycle set <campaign> <status> [flags]
```

### Examples

```
  camp lifecycle set old-project reference
```

### Options

```
  -h, --help   help for set
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp list

List all registered campaigns

### Synopsis

List all campaigns registered in the global registry.

Campaigns are registered when created with 'camp init' or manually
with 'camp register'. The registry lives at ~/.obey/campaign/registry.json.

In a terminal, 'camp list' (with no flags) opens an interactive browser where you
can deactivate/reactivate campaigns (cycle lifecycle status), reassign their org,
and copy paths. Pass an org as a positional argument to open the browser filtered
to that org. Piped, with --json/--count, or with any filter/sort flag it prints
the table instead. Home paths display as '~'.

Output formats:
  table   - Aligned columns with headers (default)
  simple  - Campaign names only, one per line
  json    - JSON array for scripting

Sorting options:
  accessed - Most recently accessed first (default)
  name     - Alphabetically by name
  type     - Alphabetically by type
  org      - By org (fallback first, then alphabetical), then by name

Examples:
  camp list                  List all campaigns
  camp list obey             Browse campaigns in the obey org
  camp list --json           Output as JSON
  camp list --format json    Output as JSON
  camp list --sort name      Sort by name
  camp list --sort org       Sort by org, then name
  camp list --format simple  Names only for scripting
  camp list --count          Print only the total number of campaigns
  camp list --remote         Also list campaigns on machines in ~/.obey/machines.yaml

--remote runs each machine's own 'camp list --json' through a login shell
(sh -lc) so PATH entries a login profile exports (~/.profile, etc.) are
picked up. If camp still can't be found on a machine, set
CAMP_REMOTE_CAMP_PATH to its exact path there.

```
camp list [org] [flags]
```

### Options

```
      --all              Show all statuses (default hides inactive/reference)
      --count            Print only the total number of campaigns
  -f, --format string    Output format (table, simple, json) (default "table")
      --group            Force org grouping
  -h, --help             help for list
  -i, --interactive      Open the interactive campaign browser (prints the table when stdout is not a terminal)
      --json             Output as JSON (shorthand for --format json)
      --no-group         Suppress org grouping
      --org string       Only campaigns in this org
      --remote           Also list campaigns on machines in ~/.obey/machines.yaml (ssh)
  -s, --sort string      Sort by (name, accessed, type, org) (default "accessed")
      --status string    Only campaigns in this status (active, inactive, reference)
      --tag strings      Only campaigns carrying this tag (repeat for AND)
      --verify-verbose   Show detailed verification output
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp log

Show git log of the campaign

### Synopsis

Show git log of the campaign root repository.

Works from anywhere within the campaign - always shows the log
of the campaign root repository.

Use --sub to show log of the submodule detected from your current directory.
Use --project/-p to show log of a specific project.

Examples:
  camp log              # Full log
  camp log -5           # Last 5 commits
  camp log --oneline    # One line per commit
  camp log --graph      # Show branch graph
  camp log --sub        # Log of current submodule
  camp log -p projects/camp --oneline  # Log of camp project

```
camp log [flags]
```

### Options

```
  -h, --help   help for log
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp machine

Manage remote machines (~/.obey/machines.yaml)

### Synopsis

Manage the fleet of remote machines camp can reach for 'camp switch machine:campaign'
and 'camp list --remote'.

Machines are stored in ~/.obey/machines.yaml. The current machine is always
implicitly available as "local" and is never written to that file.

'camp machine diagnose' inspects the per-machine ssh ControlMaster sockets and
can clear a stale one (the state a sleep or network flap can leave behind, which
would otherwise hang the next hop until ControlPersist expires).

### Examples

```
  camp machine list
  camp machine add devbox --host devbox.tailnet.ts.net --auth tailscale-ssh
  camp machine add --discover
  camp machine remove devbox
  camp machine diagnose
  camp machine diagnose devbox --reset
```

### Options

```
  -h, --help   help for machine
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp machine add

Add or update a machine

### Synopsis

Add a machine to ~/.obey/machines.yaml, or update it if the id already exists
(idempotent on id: a second 'add' with the same id replaces the entry rather
than duplicating it).

With --discover, camp runs 'tailscale status --json' and lets you pick a
tailnet device instead of specifying --host/--auth by hand; the chosen device
is saved with auth_method=tailscale-ssh. Pass an id positionally with
--discover to select that device by its derived id non-interactively (skips
the picker), or use --yes to take the first discovered device.

```
camp machine add [id] [flags]
```

### Examples

```
  camp machine add devbox --host devbox.tailnet.ts.net --auth tailscale-ssh
  camp machine add buildbox --host 10.0.0.12 --auth ssh-agent --user ci
  camp machine add --discover
  camp machine add devbox --discover
  camp machine add --discover --yes
```

### Options

```
      --auth string       Auth method: tailscale-ssh, ssh-agent, ssh-password (default "ssh-agent")
      --discover          Discover devices via 'tailscale status --json' and pick one
  -h, --help              help for add
      --host string       SSH host or Tailscale MagicDNS name (required unless --discover)
      --identity string   Path to SSH identity file
      --label string      Human-readable label
      --user string       SSH user
      --yes               With --discover, take the first discovered device non-interactively
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp machine diagnose

Inspect (and optionally clear) ssh ControlMaster sockets

### Synopsis

Report the ssh ControlMaster multiplex socket state for each configured machine
(or one machine if an id is given):

  none   no socket — the next hop opens a fresh master
  live   socket present and the master answers 'ssh -O check'
  stale  socket present but the master no longer answers

A stale socket is what a sleep or network flap can leave behind; until it is
removed (or ControlPersist expires) the next 'camp switch machine:...' or
'camp list --remote' hop to that machine can hang. Pass --reset to tear down
stale sockets so the next hop reconnects cleanly. Live and absent sockets are
left untouched.

```
camp machine diagnose [id] [flags]
```

### Examples

```
  camp machine diagnose
  camp machine diagnose devbox
  camp machine diagnose --reset
  camp machine diagnose devbox --reset --json
```

### Options

```
  -h, --help    help for diagnose
      --json    Output as JSON
      --reset   Tear down stale ControlMaster sockets so the next hop reconnects
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp machine list

List configured machines

### Synopsis

List every machine in ~/.obey/machines.yaml, plus the implicit "local" machine
(this machine, never persisted to the file).

```
camp machine list [flags]
```

### Options

```
  -h, --help   help for list
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp machine remove

Remove a machine

### Synopsis

Remove a machine from ~/.obey/machines.yaml. Removing "local" or an unknown id is an error.

```
camp machine remove <id> [flags]
```

### Options

```
  -h, --help   help for remove
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp move

Move a file or directory within the campaign

### Synopsis

Move a file or directory within the current campaign.

Paths are resolved relative to the current directory, matching standard
'mv' behavior and tab completion.

Use @ prefix for campaign shortcuts (e.g., @p/fest, @f/active/).
Available shortcuts are defined in campaign config.

If the destination is an existing directory or ends with '/', the source
is placed inside it with the same basename.

```
camp move <src> <dest> [flags]
```

### Examples

```
  camp move mydir/ ../docs/mydir/
  camp mv @f/active/old-fest @f/completed/
  camp mv draft.md @w/design/
```

### Options

```
  -f, --force   Overwrite destination without prompting
  -h, --help    help for move
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org

Group campaigns into orgs

### Synopsis

Group related campaigns into first-class orgs.

Every campaign belongs to exactly one org (default "default"). Orgs are first-class:
they persist in the machine-wide registry, can hold zero members, and are deleted
explicitly with 'camp org delete'.

In a terminal, 'camp org' (no arguments) opens an interactive browser of orgs
and their members where you can move, create, rename, and return campaigns. When
piped or with --json it prints the current campaign's org instead; use
'camp org which' to print the org unconditionally.

Commands:
  which   Print the current campaign's org
  create  Create an org (optionally --empty) and optionally join campaigns
  add     Assign campaigns to an org (also reassigns; single-membership)
  remove  Return campaigns to the default org
  delete  Delete an org (empty only unless --force)

```
camp org [flags]
```

### Examples

```
  camp org                                       Browse and manage orgs interactively (TTY)
  camp org which                                 Print the current campaign's org
  camp org create obey                           Add the current campaign to "obey"
  camp org create empty-org --empty              Create an org with no members
  camp org add obey obey-campaign obey-content   Move campaigns into "obey"
  camp org remove obey-content                   Return a campaign to "default"
  camp org delete empty-org                      Delete an empty org
```

### Options

```
  -h, --help          help for org
  -i, --interactive   Open the interactive org browser (prints the org list when stdout is not a terminal)
      --json          Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org add

Assign campaigns to an org (reassigns; single-membership)

### Synopsis

Assign one or more campaigns to <org>.

Membership is single, so this is also the reassign verb: a campaign added to a
new org leaves its previous org in the same step. The org is created implicitly.
Adding a campaign already in <org> is a no-op for that campaign.

```
camp org add <org> <campaign>... [flags]
```

### Examples

```
  camp org add obey obey-campaign obey-content
  camp org add client-acme acme-site --json
```

### Options

```
  -h, --help   help for add
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org create

Create an org (optionally empty) and join campaigns

### Synopsis

Create a first-class org, optionally joining campaigns to it.

Run inside a campaign with no campaign arguments to add the current campaign:
  camp org create obey

Or name the campaigns explicitly:
  camp org create obey obey-campaign obey-content

Create an empty org with no members (works outside a campaign):
  camp org create obey --empty

Orgs are first-class: they persist in the registry even with zero members.
Joining an org that already has members is allowed; there is no "already exists"
error, and a campaign already in the org is reported as unchanged.

```
camp org create <org> [campaign...] [flags]
```

### Examples

```
  camp org create obey
  camp org create obey --empty
  camp org create client-acme acme-site other-site
```

### Options

```
      --empty   Create the org with no members (do not join any campaign)
  -h, --help    help for create
      --json    Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org delete

Delete an org (empty only unless --force)

### Synopsis

Delete a first-class org from the registry.

Empty orgs delete without flags. Orgs with members require --force, which
reassigns every member to the fallback org and then deletes the org.

The fallback org cannot be deleted.

```
camp org delete <name> [flags]
```

### Examples

```
  camp org delete empty-org
  camp org delete obey --force
  camp org delete empty-org --json
```

### Options

```
      --force   Reassign members to the fallback org, then delete
  -h, --help    help for delete
      --json    Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org list

List orgs with member and active counts

```
camp org list [flags]
```

### Examples

```
  camp org list
```

### Options

```
  -h, --help   help for list
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org remove

Return campaigns to the default org

### Synopsis

Return one or more campaigns to the "default" org.

Since a campaign is always in exactly one org, you do not name the org.
Removing a campaign already in "default" is a no-op.

```
camp org remove <campaign>... [flags]
```

### Examples

```
  camp org remove obey-content
  camp org remove acme-site other-site --json
```

### Options

```
  -h, --help   help for remove
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org rename

Rename an org, reassigning all members atomically

### Synopsis

Rename <old> to <new>, reassigning every member in one atomic write.

Errors if <old> has no members or if <new> already exists (no implicit merge).
Renaming the fallback org ("default" by default) makes <new> the new fallback.

```
camp org rename <old> <new> [flags]
```

### Examples

```
  camp org rename obey obedience
```

### Options

```
  -h, --help   help for rename
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org show

Show an org's member campaigns

```
camp org show <org> [flags]
```

### Examples

```
  camp org show obey
```

### Options

```
  -h, --help   help for show
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp org which

Print the current campaign's org

```
camp org which [flags]
```

### Examples

```
  camp org which
```

### Options

```
  -h, --help   help for which
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp pin

Pin a directory

### Synopsis

Pin a directory for quick navigation with 'camp go <name>' or 'cgo <name>'.

If path is omitted, the current working directory is used.

```
camp pin <name> [path] [flags]
```

### Examples

```
  camp pin code                        # Pin current directory as "code"
  camp pin design workflow/design/my-project
  camp go code                         # Jump to a pin by name
  cgo design                           # Shell jump to a pin
```

### Options

```
  -h, --help   help for pin
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp pins

List all pinned directories

### Synopsis

List all saved pins. Use 'camp pin' to add and 'camp unpin' to remove.

```
camp pins [flags]
```

### Options

```
  -h, --help   help for pins
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp plugins

List discovered camp plugins on PATH

```
camp plugins [flags]
```

### Options

```
  -h, --help   help for plugins
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project

Manage campaign projects

### Synopsis

Manage git submodules and project repositories in the campaign.

A project can be:
  - a git repository tracked as a submodule under projects/
  - a machine-local linked workspace attached via symlink under projects/

Use 'camp project add' for submodules and 'camp project link' / 'camp project unlink'
for linked workspaces. Use 'camp project run' (or the 'cr -p' shell shorthand)
to run a command inside a project from anywhere in the campaign.

Examples:
  camp project list                    List all projects
  camp project add git@github.com:org/repo.git  Add a new project
  camp project link ~/code/my-project  Link an existing local workspace
  camp project run -p fest -- just build  Run a command inside a project
  camp project commit -p fest -m "fix"  Commit changes in a project submodule
  camp project prune                   Delete merged branches in the cwd's project
  camp project worktree add my-branch --project fest  Create a worktree for a project
  camp project remove api-service      Remove a project

```
camp project [flags]
```

### Options

```
  -h, --help   help for project
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project add

Add a project to campaign

### Synopsis

Add a git repository as a project in the campaign.

The project is cloned as a git submodule into the projects/ directory.
A worktree directory is also created for future parallel development.

If you're already inside a campaign, that campaign is used by default.
Outside a campaign, use --campaign <name-or-id> or a bare --campaign to
select a registered target campaign.

Source can be:
  - SSH URL:   git@github.com:org/repo.git
  - HTTPS URL: https://github.com/org/repo.git
  - Local path (with --local): ./existing-repo

Examples:
  camp project add git@github.com:org/api.git           # Add remote repo
  camp project add https://github.com/org/web.git       # Add via HTTPS
  camp project add --local ./my-repo --name my-project  # Add existing local repo
  camp project add --campaign platform --local ./my-repo # Add outside current campaign
  camp project add git@github.com:org/api.git --name backend  # Custom name

```
camp project add [source] [flags]
```

### Options

```
  -c, --campaign string   Target campaign by name or ID; omit value to pick interactively
  -h, --help              help for add
  -l, --local string      Add existing local repository instead of cloning
  -n, --name string       Override project name (defaults to repo name)
      --no-commit         Skip automatic git commit
  -p, --path string       Override destination path (defaults to projects/<name>)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project commit

Commit changes in a project submodule

### Synopsis

Commit changes within a project submodule.

Auto-detects the current project from your working directory,
or use --project to specify a project by name.

Examples:
  # From within a project directory
  cd projects/my-api
  camp project commit -m "Fix bug"

  # Specify project by name
  camp project commit --project my-api -m "Update deps"

```
camp project commit [flags]
```

### Options

```
  -a, --all                   Stage all changes (default true)
      --amend                 Amend the previous commit
      --auto-write            Run configured commit message writer
  -h, --help                  help for commit
  -m, --message stringArray   Commit message (repeatable; multiple -m are joined git-style into subject + body; required unless --auto-write)
      --no-sync               Do not sync submodule ref even if settings enable it
  -p, --project string        Project name (auto-detected from cwd if not specified)
      --sync                  Sync submodule ref at campaign root after commit (also enabled by commit.sync_project_refs setting)
      --workitem string       explicit workitem selector for the commit tag (overrides cwd-based resolution)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project link

Link an existing local project into a campaign

### Synopsis

Link an existing local directory into a campaign.

If path is omitted, camp links the current working directory.

If you're already inside a campaign, camp uses that campaign automatically.
If you're outside a campaign in an interactive terminal, camp opens a picker
so you can choose a registered campaign. Use --campaign <name-or-id> to skip
the picker or for non-interactive scripts.

This creates a symlink at projects/<name> and writes .camp with the selected
campaign ID.

Examples:
  camp project link                          # Link current directory
  camp project link ~/code/my-project        # Link another directory
  camp project link --campaign platform      # Link current directory to a specific campaign
  camp project link ~/code/my-project --campaign platform
  camp project link ~/code/my-project --name backend

```
camp project link [path] [flags]
```

### Options

```
  -c, --campaign string   Target campaign by name or ID; defaults to current campaign or interactive picker
  -h, --help              help for link
  -n, --name string       Override project name (defaults to directory name)
      --no-commit         Skip automatic git commit
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project list

List projects in campaign

### Synopsis

List all projects in the current campaign.

Projects are discovered from the projects/ directory. They may be regular
git-backed entries or linked external directories.

Output formats:
  table   - Aligned columns with headers (default)
  simple  - Project names only, one per line
  json    - JSON array for scripting

Examples:
  camp project list               List projects in table format
  camp project list --json        Output as JSON
  camp project list --format json Output as JSON
  camp project list --format simple  Names only for scripting
  camp project list --count       Print only the total number of projects

```
camp project list [flags]
```

### Options

```
      --count           Print only the total number of projects
  -f, --format string   Output format (table, simple, json) (default "table")
  -h, --help            help for list
      --json            Output as JSON (shorthand for --format json)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project new

Create a new project in campaign

### Synopsis

Create a new local project as a git submodule in the campaign.

The project is initialized as a git repository with an initial commit,
then added as a submodule under projects/. No remote repository is required.

You can add a remote later:
  cd projects/<name>
  git remote add origin git@github.com:org/<name>.git

Examples:
  camp project new my-service             # Create new project
  camp project new my-service --no-commit # Skip auto-commit to campaign

```
camp project new <name> [flags]
```

### Options

```
  -h, --help          help for new
      --no-commit     Skip automatic git commit
  -p, --path string   Override destination path (defaults to projects/<name>)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project prune

Delete merged branches in a project

### Synopsis

Delete local branches that have been merged into the default branch.

Auto-detects the current project from your working directory,
or accepts a project name as a positional argument.

Protected branches (default branch, current branch) are never deleted.

Examples:
  camp project prune                     # Prune current project
  camp project prune camp                # Prune by name
  camp project prune -p camp             # Prune by flag
  camp project prune --dry-run           # Preview what would be deleted
	camp project prune --remote            # Also prune stale remote tracking refs
  camp project prune --remote-delete     # Also delete merged branches on origin

```
camp project prune [project-name] [flags]
```

### Options

```
      --discard-dirty    Allow removal of worktrees with uncommitted changes (for branches with worktrees)
  -n, --dry-run          Preview without deleting
  -f, --force            Skip local branch deletion confirmation
  -h, --help             help for prune
  -p, --project string   Project name (auto-detected from cwd)
      --remote           Also prune stale remote tracking refs
      --remote-delete    Also delete merged branches on origin (destructive)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project prune all

Delete merged branches across all projects

### Synopsis

Delete local branches that have been merged into the default branch,
across every project submodule in the campaign.

Produces a per-project summary showing what was (or would be) pruned.

Examples:
  camp project prune all                 # Prune all projects
  camp project prune all --dry-run       # Preview across all projects
  camp project prune all --force         # Skip confirmation for each project
  camp project prune all --remote        # Also prune stale remote tracking refs

```
camp project prune all [flags]
```

### Options

```
  -h, --help   help for all
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project remote

Manage remotes for a project

### Synopsis

Manage git remotes for a campaign project.

Auto-detects the current project from your working directory, or use --project
to specify explicitly.

Commands:
  list      List remotes (default)
  set-url   Update a remote URL atomically across all locations
  add       Add a new remote
  remove    Remove a remote
  rename    Rename a remote

Examples:
  # From within a project directory
  cd projects/my-api
  camp project remote                          # List remotes
  camp project remote set-url git@github.com:org/new-repo.git
  camp project remote add upstream git@github.com:org/upstream.git
  camp project remote remove upstream
  camp project remote rename upstream fork

  # With explicit project
  camp project remote list --project my-api

```
camp project remote [flags]
```

### Options

```
  -h, --help             help for remote
  -p, --project string   Project name (auto-detected from cwd if not specified)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project remote add

Add a new remote to the project

### Synopsis

Add a new git remote to the project repository.

This does NOT modify .gitmodules — use set-url to change the canonical
origin for a submodule. Use this command to add secondary remotes such
as an upstream fork or a mirror.

After adding, a git fetch is performed to verify connectivity and
report how many refs are available.

Examples:
  camp project remote add upstream git@github.com:org/upstream.git
  camp project remote add mirror https://gitlab.com/org/repo.git

```
camp project remote add <name> <url> [flags]
```

### Options

```
  -h, --help   help for add
```

### Options inherited from parent commands

```
      --no-color         disable colored output
  -p, --project string   Project name (auto-detected from cwd if not specified)
```
---

## camp project remote list

List remotes for the project

### Synopsis

List all git remotes configured for the current project.

For submodule projects, also shows whether the origin URL matches
the canonical URL declared in .gitmodules.

Examples:
  camp project remote list
  camp project remote list --project my-api

```
camp project remote list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --no-color         disable colored output
  -p, --project string   Project name (auto-detected from cwd if not specified)
```
---

## camp project remote remove

Remove a remote from the project

### Synopsis

Remove a git remote from the project repository.

Removing the "origin" remote is blocked by default because it is the
canonical remote for submodule tracking. Use --force to override.

When --force is used to remove origin from a submodule project, the
.gitmodules entry is also cleaned up to keep the campaign consistent.

Note: if you want to change the canonical URL instead of removing it,
use "camp project remote set-url".

Examples:
  camp project remote remove upstream
  camp project remote remove origin --force   # also cleans .gitmodules

```
camp project remote remove <name> [flags]
```

### Options

```
  -f, --force   Allow removing the origin remote (dangerous)
  -h, --help    help for remove
```

### Options inherited from parent commands

```
      --no-color         disable colored output
  -p, --project string   Project name (auto-detected from cwd if not specified)
```
---

## camp project remote rename

Rename a remote in the project

### Synopsis

Rename a git remote in the project repository.

Renaming away from "origin" is blocked by default for submodule projects
because submodule tracking depends on the "origin" remote name. A future
"git submodule sync" would recreate origin from .gitmodules, undoing the
rename and leaving the project in a confusing state.

Use --force to override this guard. If you need to change the URL instead,
use "camp project remote set-url".

Renaming TO "origin" is allowed and will update .gitmodules to use the
new remote's URL as the canonical submodule URL.

Examples:
  camp project remote rename upstream fork
  camp project remote rename origin old-origin --force

```
camp project remote rename <old> <new> [flags]
```

### Options

```
  -f, --force   Allow renaming away from origin (submodule tracking may break)
  -h, --help    help for rename
```

### Options inherited from parent commands

```
      --no-color         disable colored output
  -p, --project string   Project name (auto-detected from cwd if not specified)
```
---

## camp project remote set-url

Update a remote URL for the project

### Synopsis

Update a remote URL across all tracked locations with automatic rollback.

For submodule projects, updates three locations in order:
  1. .gitmodules  (canonical, tracked in git)
  2. local git submodule config (.git/config of the campaign root)
  3. remote config inside the project repo

If any step fails, previous steps are automatically rolled back to keep
all locations consistent. If rollback also fails, recovery instructions
are printed so you can fix it manually.

For non-submodule projects, only the remote config is updated.

Flags:
  --name      Remote name to update (default: origin)
  --no-verify Skip connectivity check after updating
  --no-stage  Skip auto-staging .gitmodules

Examples:
  camp project remote set-url git@github.com:org/new-name.git
  camp project remote set-url https://github.com/org/repo.git --name upstream
  camp project remote set-url git@github.com:org/repo.git --no-verify

```
camp project remote set-url <url> [flags]
```

### Options

```
  -h, --help          help for set-url
  -n, --name string   Remote name to update (default "origin")
      --no-stage      Skip auto-staging .gitmodules
      --no-verify     Skip connectivity check after updating
```

### Options inherited from parent commands

```
      --no-color         disable colored output
  -p, --project string   Project name (auto-detected from cwd if not specified)
```
---

## camp project remove

Remove a project from campaign

### Synopsis

Remove a project from the campaign.

By default, this only removes the project from git submodule tracking.
The project directory is removed from the working tree by git rm. Pass --delete
to also remove any worktree directories managed by camp.

For linked projects, prefer 'camp project unlink'. Linked projects are
machine-local symlinks and are never deleted through this command.

Use --delete to also remove all project files. This is destructive
and requires confirmation unless --force is also specified.

Examples:
  camp project remove api-service           # Unregister submodule only
  camp project remove api-service --delete  # Also delete files (confirms)
  camp project remove api-service --delete --force  # Delete without confirmation
  camp project remove api-service --dry-run # Show what would be done

```
camp project remove <name> [flags]
```

### Options

```
  -d, --delete      Also delete project files (destructive)
      --dry-run     Show what would be done without making changes
  -f, --force       Skip confirmation prompts
  -h, --help        help for remove
      --no-commit   Skip automatic git commit
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project run

Run a command inside a project directory, like cr but project-scoped

### Synopsis

Run any shell command inside a project directory from anywhere in the campaign.

This is the project-scoped counterpart to 'camp run' (cr): cr runs from the
campaign root, camp project run (cr -p) runs inside a project.

The project is resolved in this order:
  1. --project / -p flag (explicit project name, tab-completes registered projects)
  2. Auto-detect from current working directory
  3. Interactive fuzzy picker (if neither above applies)

Use -- to separate camp flags from the command to execute.

Examples:
  # Interactive project picker, then run command
  camp project run -- ls -la

  # Specify project explicitly
  camp project run -p fest -- just build
  camp project run --project camp -- go test ./...

  # Auto-detect from cwd (inside projects/fest/)
  camp project run -- just test all

  # Simple commands (no -- needed when no flags)
  camp project run make build

  # Shell shorthand (after 'eval "$(camp shell-init <shell>)"')
  cr -p fest -- just build
  cr -p camp go test ./...

```
camp project run [--project <name>] [--] <command> [args...] [flags]
```

### Options

```
  -h, --help             help for run
  -p, --project string   Project name (auto-detected from cwd, or interactive picker if omitted)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project stage

Stage changes in a project submodule

### Synopsis

Stage changes within a project submodule without committing.

Runs the same auto-staging logic as 'camp project commit' (including
stale lock file cleanup) but stops before creating a commit, so you can
use a different commit strategy.

Auto-detects the current project from your working directory,
or use --project to specify a project by name.

Examples:
  # From within a project directory
  cd projects/my-api
  camp project stage

  # Specify project by name
  camp project stage --project my-api

```
camp project stage [flags]
```

### Options

```
  -h, --help             help for stage
  -p, --project string   Project name (auto-detected from cwd if not specified)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project unlink

Unlink a linked project from a campaign

### Synopsis

Remove a linked project symlink from a campaign without touching the
external workspace contents.

If name is omitted, the current linked project is inferred from the working
directory.

Use this for linked workspaces added with 'camp project link'. This command
removes the symlink entry from projects/ and cleans up the linked repo's local
.camp marker when it belongs to the selected campaign.

If you're already inside a campaign, that campaign is used by default.
Outside a campaign, use --campaign <name-or-id> or a bare --campaign to
pick a registered target campaign interactively.

Examples:
  camp project unlink
  camp project unlink my-project
  camp project unlink my-project --campaign platform
  camp project unlink --campaign platform
  camp project unlink my-project --campaign
  camp project unlink my-project --dry-run

```
camp project unlink [name] [flags]
```

### Options

```
  -c, --campaign string   Target campaign by name or ID; omit value to pick interactively
      --dry-run           Show what would be done without making changes
  -h, --help              help for unlink
      --no-commit         Skip automatic git commit
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project worktree

Manage worktrees for a project

### Synopsis

Manage git worktrees for the current project.

Worktrees allow you to have multiple working directories for the same repository,
enabling parallel development on different branches without stashing or switching.

Auto-detects the current project from your working directory, or use --project
to specify explicitly.

All worktrees are created at: projects/worktrees/<project>/<worktree-name>/

Commands:
  add       Create a new worktree
  list      List worktrees for the project
  remove    Remove a worktree

Examples:
  # From within a project directory
  cd projects/my-api
  camp project worktree add feature-auth      # Creates new branch based on current
  camp project worktree add fix --start-point main  # New branch based on main
  camp project worktree list
  camp project worktree remove feature-auth

  # With explicit project
  camp project worktree add feature-xyz --project my-api

```
camp project worktree [flags]
```

### Options

```
  -h, --help   help for worktree
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project worktree add

Create a new worktree for the project

### Synopsis

Create a new git worktree for the current project.

Auto-detects the project from your current directory, or use --project
to specify explicitly.

The worktree will be created at: projects/worktrees/<project>/<name>/

By default, creates a new branch with the worktree name based on the current branch.
Use --branch to checkout an existing branch instead.

Examples:
  # Create worktree with new branch based on current branch (default)
  camp project worktree add feature-auth

  # Create worktree with new branch based on main
  camp project worktree add experiment --start-point main

  # Checkout existing branch (instead of creating new)
  camp project worktree add hotfix --branch hotfix-123

  # Track a remote branch
  camp project worktree add pr-review --track origin/feature-xyz

  # Explicit project
  camp project worktree add feature --project my-api

  # Link a design/explore workitem so camp p commit in the worktree tags WI-*
  camp project worktree add fest-list-watch --project fest --workitem WI-2a7950
  camp project worktree add settings-tui --project camp --workitem workflow/design/camp-settings-tui

```
camp project worktree add <name> [flags]
```

### Options

```
  -b, --branch string        Checkout existing branch instead of creating new one
  -h, --help                 help for add
  -p, --project string       Project name (auto-detected from cwd if not specified)
  -s, --start-point string   Base branch/commit for new branch (default: current branch)
  -t, --track string         Remote branch to track (creates new local tracking branch)
      --workitem string      workitem selector (ref, path, or id) to primary-link to this worktree for camp p commit tags
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project worktree list

List worktrees for the project

### Synopsis

List all worktrees for the current project.

Auto-detects the project from your current directory, or use --project
to specify explicitly.

Examples:
  # From within a project
  cd projects/my-api
  camp project worktree list

  # Explicit project
  camp project worktree list --project my-api

```
camp project worktree list [flags]
```

### Options

```
  -h, --help             help for list
  -p, --project string   Project name (auto-detected from cwd if not specified)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp project worktree remove

Remove a worktree

### Synopsis

Remove a worktree from the current project.

Auto-detects the project from your current directory, or use --project
to specify explicitly.

Examples:
  # From within a project
  cd projects/my-api
  camp project worktree remove feature-auth

  # Force remove (even with uncommitted changes)
  camp project worktree remove experiment --force

  # Explicit project
  camp project worktree remove feature --project my-api

```
camp project worktree remove <name> [flags]
```

### Options

```
  -f, --force            Force removal even with uncommitted changes
  -h, --help             help for remove
  -p, --project string   Project name (auto-detected from cwd if not specified)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp promote

Promote any intent, workitem, or festival (universal front door)

```
camp promote [id] [flags]
```

### Options

```
      --dest string     Destination override (doc/festival targets)
      --dry-run         Preview without making changes
      --force           Skip readiness checks
      --goal string     Festival goal override
  -h, --help            help for promote
      --json            Machine-readable output; implies non-interactive
      --keep            Keep the source (festival/doc targets)
      --no-commit       Skip auto-commit
      --target string   Promote target (kind-specific); required in non-interactive mode
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp pull

Pull latest changes from remote

### Synopsis

Pull latest changes from the remote repository.

Works from anywhere within the campaign - always pulls to
the campaign root repository.

Use --sub to pull the submodule detected from your current directory.
Use --project to pull a specific project.
Use 'camp pull all' to pull all repos with upstream tracking.

Any git pull flags are passed through (e.g. --rebase, --ff-only).

Examples:
  camp pull                    # Pull current branch (merge)
  camp pull --rebase           # Pull with rebase
  camp pull --ff-only          # Fast-forward only
  camp pull --sub              # Pull current submodule
  camp pull --project=projects/camp  # Pull camp project
  camp pull all                # Pull all repos
  camp pull all --ff-only      # Pull all repos, fast-forward only

```
camp pull [flags] [remote] [branch]
```

### Options

```
  -h, --help   help for pull
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp pull all

Pull latest changes for all repos

### Synopsis

Pull latest changes for all repositories in the campaign.

Scans the campaign root and all submodules, checks which have a tracking
branch with upstream, and pulls them. Any extra flags are passed through
to git pull for each repo.

Repos in detached HEAD state or without upstream tracking are skipped.
Use --default-branch to auto-checkout each submodule's default branch
before pulling. This is useful when submodules are on stale feature
branches whose remote tracking branch has been deleted.

By default, nested submodules (e.g. inside monorepos) are included.
Use --no-recurse to only pull top-level submodules.

Examples:
  camp pull all                      # Pull all repos
  camp pull all --rebase             # Pull all repos with rebase
  camp pull all --ff-only            # Fast-forward only for all repos
  camp pull all --no-recurse         # Only top-level submodules
  camp pull all --default-branch     # Checkout default branch first

```
camp pull all [git pull flags] [flags]
```

### Options

```
  -h, --help   help for all
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp push

Push campaign changes to remote

### Synopsis

Push campaign changes to the remote repository.

Works from anywhere within the campaign - always pushes from
the campaign root repository.

Use --sub to push from the submodule detected from your current directory.
Use --project to push from a specific project.
Use 'camp push all' to push all repos that have unpushed commits.

Examples:
  camp push                    # Push current branch
  camp push origin main        # Push to specific remote/branch
  camp push --force            # Force push
  camp push -u origin feature  # Push and set upstream
  camp push --sub              # Push current submodule
  camp push --project=projects/camp  # Push camp project
  camp push all                # Push all repos with unpushed commits
  camp push all --force        # Force push all repos

```
camp push [flags] [remote] [branch]
```

### Options

```
  -h, --help   help for push
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp push all

Push all repos with unpushed commits

### Synopsis

Push all repositories in the campaign that have unpushed commits.

Scans all submodules and the campaign root, checks which have commits
ahead of their upstream, and pushes them. Any extra flags are passed
through to git push for each repo.

By default, nested submodules (e.g. inside monorepos) are included.
Use --no-recurse to only push top-level submodules.

Examples:
  camp push all              # Push all repos with unpushed commits
  camp push all --force      # Force push all repos
  camp push all -u origin    # Push and set upstream for all
  camp push all --no-recurse # Only top-level submodules

```
camp push all [git push flags] [flags]
```

### Options

```
  -h, --help   help for all
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp refs-sync

Sync submodule ref pointers in campaign root

### Synopsis

Update the campaign root's recorded submodule pointers to match
each submodule's current HEAD. Creates a single atomic commit.

Without arguments, syncs all submodules. Specify paths to sync specific ones.

Examples:
  camp refs-sync                      # Sync all dirty refs
  camp refs-sync projects/camp        # Sync specific submodule
  camp refs-sync --dry-run            # Show plan without executing

```
camp refs-sync [submodule...] [flags]
```

### Options

```
  -n, --dry-run   Show plan without executing
  -f, --force     Skip safety checks (staged changes)
  -h, --help      help for refs-sync
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp register

Register campaign in global registry

### Synopsis

Register an existing campaign in the global registry.

This adds the campaign to the registry at ~/.obey/campaign/registry.json,
enabling it to appear in 'camp list' and be accessible via navigation commands.

Note: 'camp init' automatically registers new campaigns. This command is for
registering existing campaigns that weren't created with camp or were unregistered.

If the specified path is not a campaign (has no .campaign/ directory),
you'll be offered the option to initialize it.

Examples:
  camp register                          # Register current directory
  camp register ~/Dev/my-project         # Register specified path
  camp register . --name custom-name     # Override the campaign name
  camp register . --type research        # Override the campaign type

```
camp register [path] [flags]
```

### Options

```
  -h, --help          help for register
  -n, --name string   Override campaign name
  -t, --type string   Override campaign type (product, research, tools, personal)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp registry

Manage the campaign registry

### Synopsis

Manage the campaign registry at ~/.obey/campaign/registry.json.

The registry tracks all known campaigns for quick navigation and lookup.
Use these commands to maintain registry health and resolve issues.

Commands:
  prune   Remove stale entries (campaigns that no longer exist)
  sync    Update registry entry for current campaign
  check   Validate registry integrity

```
camp registry [flags]
```

### Examples

```
  camp registry prune             Remove entries for non-existent campaigns
  camp registry prune --dry-run   Show what would be removed
  camp registry sync              Update path for current campaign
  camp registry check             Check for issues
```

### Options

```
  -h, --help   help for registry
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp registry check

Check registry integrity

### Synopsis

Validate the registry and report any issues found.

Checks for:
- Stale entries (paths that don't exist)
- Missing .campaign/ directories
- Campaigns in /tmp/ directories
- Duplicate entries (multiple IDs pointing to the same path)

Examples:
  camp registry check   Show any registry issues

```
camp registry check [flags]
```

### Options

```
  -h, --help   help for check
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp registry prune

Remove stale registry entries

### Synopsis

Remove registry entries where the campaign no longer exists.

Checks each registered path and removes entries where:
- The path no longer exists
- The path has no .campaign/ directory

Options:
  --dry-run       Show what would be removed without making changes
  --include-temp  Also remove entries in /tmp/ directories

Examples:
  camp registry prune             Remove stale entries
  camp registry prune --dry-run   Preview what would be removed
  camp registry prune --include-temp  Also clean up test campaigns

```
camp registry prune [flags]
```

### Options

```
      --dry-run        Show what would be removed without making changes
  -h, --help           help for prune
      --include-temp   Also remove entries in /tmp/ directories
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp registry sync

Sync current campaign with registry

### Synopsis

Update the registry entry for the current campaign.

Run this after moving a campaign directory to update its path
in the registry. Reads the campaign ID from .campaign/campaign.yaml
and updates (or adds) the registry entry.

Examples:
  camp registry sync   # Run from inside a campaign

```
camp registry sync [flags]
```

### Options

```
  -h, --help   help for sync
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp root

Print the current campaign root

### Synopsis

Print the current campaign root relative to the current working directory.

```
camp root [flags]
```

### Examples

```
  camp root
  camp root --json
```

### Options

```
  -h, --help   help for root
      --json   output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp run

Execute command from campaign root, or just recipe in a project

### Synopsis

Execute any command from the campaign root directory, or run just recipes
in a project directory.

If the first argument exactly matches a project name (a directory in projects/
with a git repo), camp dispatches to 'just' in that project's directory.
Any remaining arguments are passed as the recipe and arguments to just.

If the first argument does not match a project, it is treated as a shell command
and executed from the campaign root directory.

Use @shortcut prefix to run from a shortcut's directory instead of root.
Only navigation shortcuts (those with paths) can be used.

Raw command arguments after 'run' (or '@shortcut') are passed directly to the
shell. Project just-dispatch passes recipe arguments directly to just.

```
camp run [project | @shortcut] [command | recipe] [args...] [flags]
```

### Examples

```
  # Project just dispatch (first arg matches a project name):
  camp run camp              # Show just recipes for camp project
  camp run camp test all     # Run 'just test all' in projects/camp/
  camp run festival build    # Run 'just build' in projects/festival/

  # Raw command from campaign root (first arg is not a project):
  camp run just --list       # Show just recipes from root
  camp run git status        # Run git status from campaign root
  camp run ls -la            # List campaign root contents

  # Shortcut-based execution:
  camp run @p ls             # List projects/ directory
  camp run @f make test      # Run make from festivals/
```

### Options

```
  -h, --help   help for run
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp settings

Manage camp configuration

### Synopsis

Interactive menu for managing camp configuration.

Global settings live in ~/.obey/campaign/config.json and apply to every
campaign. Local settings live in .campaign/settings/local.json and apply
only to the current campaign; a local theme override wins over the global
theme while you are inside that campaign.

For non-interactive access, use 'camp settings get' and
'camp settings set'. See docs/campaign-settings-files.md in the camp
repository for the file layout.

```
camp settings [flags]
```

### Examples

```
  camp settings                              # Interactive settings menu
  camp settings get                          # Print all settings
  camp settings set global.theme dark        # Set the global theme
  camp settings set local.theme_override light
```

### Options

```
  -h, --help   help for settings
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp settings get

Print camp settings

### Synopsis

Print camp settings non-interactively.

With no key, prints all settings including the effective theme. With a key,
prints just that value.

Keys:
  global.theme               Color theme in ~/.obey/campaign/config.json
  global.editor              Preferred editor
  global.campaigns_dir       Where camp create places new campaigns
  global.verbose             Verbose output
  global.no_color            Disable colored output
  global.commit.sync_project_refs   When true, camp p commit updates campaign-root submodule pointer (default false)
  global.commit.disable_commit_tags When true, skip [campaign:…] tags on camp commits (default false; tags on)
  local.theme_override       Campaign-local theme override (requires a campaign)
  local.commit.sync_project_refs    Campaign override for project-ref sync (true/false/inherit)
  local.commit.disable_commit_tags  Campaign override to skip commit subject tags (true/false/inherit)
  local.campaign.name        Campaign name in .campaign/campaign.yaml
  local.campaign.description Campaign description
  local.campaign.mission     Campaign mission
  local.campaign.type        Campaign type (product, research, tools, personal)
  local.campaign.commit_hook Commit-message hook command
  effective.commit.*         Resolved commit prefs (get only; local overrides global)

The campaign.yaml list and tree fields (intents.tags, concepts) have no flat
key and are edited only through the interactive 'camp settings' TUI.

```
camp settings get [key] [flags]
```

### Examples

```
  camp settings get
  camp settings get global.theme
  camp settings get effective.commit.sync_project_refs
  camp settings get --json
```

### Options

```
  -h, --help   help for get
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp settings set

Set a camp setting

### Synopsis

Set a camp setting non-interactively.

Accepts the same keys as 'camp settings get'. Theme values are one of
adaptive, light, dark, or high-contrast. Boolean values accept true/false.
Setting local.theme_override to 'inherit' clears the override; local.* keys
require running inside a campaign.

```
camp settings set <key> <value> [flags]
```

### Examples

```
  camp settings set global.theme dark
  camp settings set global.verbose true
  camp settings set local.theme_override light
  camp settings set local.theme_override inherit
```

### Options

```
  -h, --help   help for set
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp shell-init

Output shell initialization code

### Synopsis

Output shell initialization code for your shell config.

Add to your shell config:
  zsh:  eval "$(camp shell-init zsh)"
  bash: eval "$(camp shell-init bash)"
  fish: camp shell-init fish | source

This provides:
  - A camp shell function that wraps the camp binary
  - cgo function for navigation
  - Tab completion for camp commands
  - Category shortcuts (p, c, f, etc.)

IMPORTANT: this defines a shell function named 'camp' that wraps the camp
binary. The function intercepts 'camp switch' and 'camp go' to perform
directory changes in the current shell session.

The following shell aliases and functions are also installed:
  cr     camp run (run a just recipe in a project)
  csw    camp switch (shorthand)
  cint   camp intent add (quick idea capture)
  cnote  camp intent note (add a note to an existing intent)
  cie    camp intent explore (interactive intent browser)

The cgo function enables quick navigation:
  cgo                 Interactive picker or jump to campaign root
  cgo p               Jump to projects/
  cgo p api           Fuzzy find "api" in projects/
  cgo -c p ls         Run "ls" in projects/ directory

```
camp shell-init <shell> [flags]
```

### Examples

```
  # Add to ~/.zshrc
  eval "$(camp shell-init zsh)"

  # Add to ~/.bashrc
  eval "$(camp shell-init bash)"

  # Add to ~/.config/fish/config.fish
  camp shell-init fish | source
```

### Options

```
  -h, --help   help for shell-init
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp shortcuts

List all available shortcuts

### Synopsis

List all navigation and command shortcuts from .campaign/settings/jumps.yaml.

Navigation shortcuts (path-based):
  These shortcuts jump to directories within the campaign.
  Usage: camp go <shortcut>

Command shortcuts (command-based):
  These shortcuts execute commands from specified directories.
  Usage: camp run <shortcut> [args...]

Default shortcuts are added when you run 'camp init'.
You can customize shortcuts by editing .campaign/settings/jumps.yaml.

```
camp shortcuts [flags]
```

### Examples

```
  camp shortcuts              # List all shortcuts
  camp go api                 # Use navigation shortcut
  camp run build              # Use command shortcut
```

### Options

```
  -h, --help   help for shortcuts
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp shortcuts add

Add a shortcut (campaign-level or project sub-shortcut)

### Synopsis

Add a shortcut for quick navigation.

Campaign-level shortcut (2 args):
  Adds a navigation shortcut to .campaign/settings/jumps.yaml.
  Usage: camp shortcuts add <name> <path>

Project sub-shortcut (3 args):
  Adds a sub-directory shortcut within a project.
  Usage: camp shortcuts add <project> <name> <path>

With no arguments, launches an interactive TUI for entering
shortcut details.

```
camp shortcuts add <name> <path> | <project> <name> <path> [flags]
```

### Examples

```
  camp shortcuts add                                  Interactive TUI mode
  camp shortcuts add api projects/api-service/        Campaign shortcut
  camp shortcuts add api projects/api/ -d "API svc"   With description
  camp shortcuts add cfg "" -c config                 Concept-only shortcut
  camp shortcuts add camp default cmd/camp/            Project sub-shortcut
```

### Options

```
  -c, --concept string       Command group for expansion
  -d, --description string   Help text for the shortcut
  -h, --help                 help for add
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp shortcuts diff

Show differences between current and default shortcuts

### Synopsis

Compare your campaign's shortcuts against the current defaults.

Shows:
  + Missing    defaults not in your config (available to add)
  - Stale      auto-generated shortcuts no longer in defaults
  ~ Modified   shortcuts where path or concept differs from default
  = Up to date shortcuts matching defaults (count only)
  * Custom     user-defined shortcuts (always preserved)

Run 'camp shortcuts reset' to apply missing defaults and remove stale entries.

```
camp shortcuts diff [flags]
```

### Examples

```
  camp shortcuts diff
```

### Options

```
  -h, --help   help for diff
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp shortcuts list

List shortcuts for a specific project

### Synopsis

List all sub-shortcuts configured for a specific project.

If no project is specified, lists all campaign shortcuts.

```
camp shortcuts list [project] [flags]
```

### Examples

```
  camp shortcuts list festival-methodology
  camp shortcuts list fest  # Fuzzy match
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp shortcuts remove

Remove a shortcut (campaign-level or project sub-shortcut)

### Synopsis

Remove a shortcut.

Campaign-level shortcut (1 arg):
  Usage: camp shortcuts remove <name>

Project sub-shortcut (2 args):
  Usage: camp shortcuts remove <project> <name>

```
camp shortcuts remove <name> or <project> <name> [flags]
```

### Examples

```
  camp shortcuts remove api                           Remove campaign shortcut
  camp shortcuts remove festival-methodology cli      Remove project sub-shortcut
```

### Options

```
  -h, --help   help for remove
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp shortcuts reset

Reset auto-generated shortcuts to current defaults

### Synopsis

Reset shortcuts to match current defaults while preserving user-defined shortcuts.

Default behavior:
  - Adds missing default shortcuts
  - Removes stale auto-generated shortcuts (no longer in defaults)
  - Updates modified auto-generated shortcuts to match defaults
  - Preserves all user-defined shortcuts

With --all:
  - Replaces entire shortcuts config with defaults
  - Removes all user-defined shortcuts (with confirmation)

With --dry-run:
  - Shows what would change without saving

```
camp shortcuts reset [flags]
```

### Examples

```
  camp shortcuts reset             # Reset auto shortcuts, preserve custom
  camp shortcuts reset --dry-run   # Preview changes
  camp shortcuts reset --all       # Full reset (drops custom shortcuts)
```

### Options

```
      --all       Reset all shortcuts including user-defined ones
      --dry-run   Show what would change without saving
  -h, --help      help for reset
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp skills

Manage campaign skill directory links

### Synopsis

Manage campaign skill bundle projection for tool interoperability.

Skills are centralized in .campaign/skills/ and projected into tool ecosystems
(Claude, agents, etc.) as per-bundle symlinks. This keeps a single source of
truth while preserving existing provider-native skills directories.

Commands:
  link     Project per-skill symlinks into a tool-specific skills directory
  status   Show projection status for tool-specific skills directories
  unlink   Remove projected skill symlinks

Examples:
  camp skills link --tool claude    Project skills into .claude/skills/
  camp skills link --tool agents    Project skills into .agents/skills/
  camp skills status                Show all skill projection states
  camp skills unlink --tool claude  Remove projected symlinks from .claude/skills/

```
camp skills [flags]
```

### Options

```
  -h, --help   help for skills
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp skills link

Project campaign skill bundles into tool-specific skills directories

### Synopsis

Project campaign skill bundles from .campaign/skills/ into tool-specific
skills directories.

This command creates one symlink per skill bundle. It does not replace entire
provider skills directories, so existing user skills remain intact.

With neither --tool nor --path, skills are projected into every registered tool.

Examples:
  camp skills link                     Project skills into all registered tools
  camp skills link --tool claude       Project skills into .claude/skills/
  camp skills link --tool agents       Project skills into .agents/skills/
  camp skills link --path custom/dir   Project skills into custom/dir
  camp skills link --tool claude -n    Dry run — show what would happen
  camp skills link --tool claude -f    Replace conflicting symlink entries

```
camp skills link [flags]
```

### Options

```
  -n, --dry-run       Show what would happen without making changes
  -f, --force         Replace conflicting symlink entries (never files/directories)
  -h, --help          help for link
  -p, --path string   Custom destination directory
  -t, --tool string   Tool to link: claude, agents
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp skills status

Show the current state of projected skill bundle symlinks

### Synopsis

Show projection status for campaign skill bundles across tool targets.

Reports whether each tool's skills directory has projected entries from
.campaign/skills/, is partially projected, missing, broken, or blocked.

Examples:
  camp skills status          Show projection states in table format
  camp skills status --json   Machine-readable JSON output

```
camp skills status [flags]
```

### Options

```
  -h, --help     help for status
      --json     Output as JSON
      --strict   Return non-zero exit code when links need attention (for CI)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp skills unlink

Remove projected skill bundle symlinks

### Synopsis

Remove managed skill bundle symlinks created by 'camp skills link'.

Only removes projected symlink entries created from .campaign/skills bundles.
It never removes non-symlink files/directories or foreign symlinks.

Examples:
  camp skills unlink --tool claude       Remove projected entries in .claude/skills/
  camp skills unlink --tool agents       Remove projected entries in .agents/skills/
  camp skills unlink --path custom/dir   Remove projected entries in custom/dir
  camp skills unlink --tool claude -n    Dry run — show what would happen

```
camp skills unlink [flags]
```

### Options

```
  -n, --dry-run       Show what would happen without making changes
  -h, --help          help for unlink
  -p, --path string   Custom destination directory to unlink
  -t, --tool string   Tool to unlink: claude, agents
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp stage

Stage changes in the campaign root

### Synopsis

Stage changes in the campaign root directory without committing.

Runs the same auto-staging logic as 'camp commit' (including stale lock
file cleanup) but stops before creating a commit, so you can use a
different commit strategy (interactive 'git commit --patch', a GUI
client, signing flow, etc.).

At the campaign root, submodule ref changes (projects/*) are excluded
from staging by default to prevent accidental ref conflicts across
machines. Use --include-refs to stage them explicitly.

Use --sub to stage in the submodule detected from your current directory.
Use -p/--project to stage in a specific project (e.g., -p projects/camp).

Examples:
  camp stage
  camp stage --include-refs
  camp stage --sub
  camp stage -p projects/camp

```
camp stage [flags]
```

### Options

```
  -h, --help             help for stage
      --include-refs     Include submodule ref changes when staging at campaign root
  -p, --project string   Operate on a specific project/submodule path
      --sub              Operate on the submodule detected from current directory
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp status

Show git status of the campaign

### Synopsis

Show git status of the campaign root directory.

Works from anywhere within the campaign - always shows the status
of the campaign root repository.

Use --sub to show status of the submodule detected from your current directory.
Use --project/-p to show status of a specific project.
Pass git status flags after -- to forward them directly to git.

```
camp status [flags] [-- <git-flags>]
```

### Examples

```
  camp status           # Full status
  camp status -s        # Short format
  camp status --sub     # Status of current submodule
  camp status -p projects/camp  # Status of camp project
```

### Options

```
  -h, --help             help for status
  -p, --project string   Status of a specific project path
  -s, --short            Give output in short format
      --show-refs        Show campaign root submodule ref changes
      --sub              Status of the submodule detected from current directory
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp status all

Show git status of all submodules

### Synopsis

Show a visual overview of git status for all submodules in the campaign.

Displays a table with each submodule's name, branch, clean/dirty state,
and push status.

Examples:
  camp status all               # Show all submodule statuses
  camp status all --remote-url  # Show remote URLs instead of names
  camp status all --json        # Output as JSON

```
camp status all [flags]
```

### Options

```
  -h, --help         help for all
      --json         Output as JSON
      --no-recurse   Only list top-level submodules
      --remote-url   Show remote URLs instead of remote names
      --view         Open interactive TUI viewer
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp switch

Switch to a different campaign

### Synopsis

Switch to a registered campaign by name or ID.

Without arguments, opens an interactive picker to select a campaign.
With an argument, looks up the campaign by name or ID prefix.
Use --org or org/campaign to resolve inside one organization.

Use with the cgo shell function for instant navigation:
  cgo switch                 # Interactive campaign picker
  cgo switch my-campaign     # Switch by name
  cgo switch a1b2             # Switch by ID prefix
  cgo switch obey/platform    # Switch by org-scoped selector

The --print flag outputs just the path for shell integration:
  cd "$(camp switch --print)"

Use campaign@tab to navigate to a specific location in the target campaign:
  camp switch obey-campaign@p    # Switch and navigate to projects/
  camp switch obey/platform@f    # Switch inside org and navigate to festivals/

Use machine:campaign to resolve a campaign on a machine registered in
~/.obey/machines.yaml (via the csw shell wrapper, which hops there over ssh):
  csw devbox:obey-campaign       # Resolve and hop to obey-campaign on devbox

Remote resolution runs the far machine's own 'camp switch' through a login
shell (sh -lc) so PATH entries a login profile exports (~/.profile, etc.) are
picked up. If camp still can't be found there, set CAMP_REMOTE_CAMP_PATH to
its exact path on that machine.

```
camp switch [campaign] [flags]
```

### Examples

```
  camp switch                        # Interactive picker
  camp switch obey-campaign          # Switch by name
  camp switch --org obey platform    # Switch by name within an org
  camp switch obey/platform          # Switch by scoped selector
  camp switch a1b2                   # Switch by ID prefix
  camp switch --print                # Picker, output path only
  camp switch obey-campaign@p        # Switch and navigate to projects/
  camp switch --all old-reference    # Include inactive/reference campaigns
  camp switch --org obey platform --json
```

### Options

```
      --all             Include inactive and reference campaigns
  -h, --help            help for switch
      --json            Output selected campaign and target path as JSON
      --org string      Only switch among campaigns in this org
      --print           Print path only (for shell integration)
      --status string   Only switch among campaigns with this lifecycle status
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp sync

Safely synchronize submodules

### Synopsis

Synchronize submodules with pre-flight safety checks.

The sync command performs three critical operations:

  1. PRE-FLIGHT CHECKS
     Verifies no uncommitted changes or unpushed commits that could
     be lost during synchronization.

  2. URL SYNCHRONIZATION
     Copies URLs from .gitmodules to .git/config, fixing URL mismatches
     that occur when remote URLs change.

  3. SUBMODULE UPDATE
     Fetches and checks out the correct commits for all submodules.

This order is critical: sync-before-update prevents silent code deletion
when URLs change on remote repositories.

EXIT CODES:
  0  Success
  1  Runtime failure (including pre-flight, sync, or update failure)
  2  Usage error (bad flags or args)
  3  Post-sync validation failed

EXAMPLES:
  # Sync all submodules (recommended default)
  camp sync

  # Preview what would happen without making changes
  camp sync --dry-run

  # Sync a specific submodule only
  camp sync projects/camp

  # Force sync despite uncommitted changes (dangerous!)
  camp sync --force

  # Detailed output for each submodule
  camp sync --verbose

  # JSON output for scripting
  camp sync --json

  # Accelerate over a peer machine from ~/.obey/machines.yaml: for each
  # already-initialized submodule, fetch objects from that machine first
  # (LAN/tailnet), then run the normal origin-based update, then pull
  # declared artifact roots (policy=always) from the same machine.
  # Uninitialized submodules skip the peer step and init from origin.
  # Preflight, origin URLs, validation, and exit codes are unchanged; an
  # unreachable peer degrades to a warning.
  camp sync --from studio-mac

  # Peer git objects only, skip artifacts / artifacts only, skip git phases
  camp sync --from studio-mac --git-only
  camp sync --from studio-mac --artifacts-only

  # Check artifact roots against last-transfer snapshots, no transfer
  camp sync --verify-artifacts
  camp sync --verify-artifacts --from studio-mac

```
camp sync [submodule...] [flags]
```

### Options

```
      --artifacts-only     With --from: pull declared artifact roots only, skip git phases
  -n, --dry-run            Show what would happen without making changes
  -f, --force              Skip safety checks (uncommitted changes warning still shown)
      --from string        Fetch objects for already-initialized submodules (and declared artifact roots) from this machine (id from ~/.obey/machines.yaml)
      --git-only           With --from: move git objects only, skip artifact roots
  -h, --help               help for sync
      --json               Output results as JSON for scripting
      --no-fetch           Skip fetching from remote (use local refs only)
  -p, --parallel int       Number of parallel git operations (git guards superproject ops with repo lockfiles that fail fast on contention; lower this if a slow disk surfaces transient lock errors) (default 4)
  -v, --verbose            Show detailed output for each submodule
      --verify-artifacts   Check artifact roots against last-transfer snapshots (no transfer)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp tag

Label campaigns with tags

### Synopsis

Label campaigns with tags from a single global pool.

Tags are orthogonal to orgs: any campaign can carry any tag regardless of its
org, and the same tag can appear across orgs. Tags are a set per campaign
(re-adding is a no-op).

Commands:
  add   Add tags to a campaign
  rm    Remove tags from a campaign
  list  List all tags in use with counts

```
camp tag [flags]
```

### Examples

```
  camp tag add obey-campaign paid-work q3-2026
  camp tag rm obey-campaign q3-2026
  camp tag list
```

### Options

```
  -h, --help   help for tag
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp tag add

Add tags to a campaign

### Synopsis

Add one or more tags to a campaign (set semantics).

Re-adding a tag the campaign already carries is a no-op for that tag. Each tag
name must be lowercase letters, digits, and hyphens with no leading digit.

```
camp tag add <campaign> <tag>... [flags]
```

### Examples

```
  camp tag add obey-campaign paid-work q3-2026
```

### Options

```
  -h, --help   help for add
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp tag list

List all tags in use with campaign counts

```
camp tag list [flags]
```

### Examples

```
  camp tag list
```

### Options

```
  -h, --help   help for list
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp tag rm

Remove tags from a campaign

### Synopsis

Remove one or more tags from a campaign.

Removing a tag the campaign does not carry is a no-op for that tag.

```
camp tag rm <campaign> <tag>... [flags]
```

### Examples

```
  camp tag rm obey-campaign q3-2026
```

### Options

```
  -h, --help   help for rm
      --json   Output as JSON
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp transfer

Copy files between campaigns

### Synopsis

Copy files between different campaigns using campaign:path syntax.

Transfer always copies — it never moves or deletes the source.
Either the source or destination (or both) can use "campaign:path"
notation to reference a different registered campaign. Paths without
a campaign prefix resolve relative to the current campaign root.

At least one side must reference a different campaign. For copies
within the same campaign, use 'camp copy' instead.

```
camp transfer <src> <dest> [flags]
```

### Examples

```
  camp transfer docs/my-doc.md other-campaign:docs/my-doc.md     # push
  camp transfer other-campaign:docs/my-doc.md docs/              # pull
  camp transfer other:festivals/plan.md festivals/planned/       # pull into dir
```

### Options

```
  -f, --force   Overwrite destination without prompting
  -h, --help    help for transfer
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp unpin

Remove a saved pin

### Synopsis

Remove a saved pin by name.

Without arguments, detects and unpins the current directory.

```
camp unpin [name] [flags]
```

### Options

```
  -h, --help   help for unpin
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp unregister

Remove campaign from registry

### Synopsis

Remove a campaign from the global registry.

This does NOT delete any files - it only removes the campaign from
tracking in the global registry. Use this when:
  - A campaign directory was deleted manually
  - A campaign was moved to a different location
  - You no longer want to track a campaign

The campaign files remain untouched on disk.

You can specify the campaign by name or ID (or ID prefix).

Examples:
  camp unregister old-project            # Remove by name
  camp unregister 550e84                 # Remove by ID prefix
  camp unregister old-project --force    # Remove without confirmation

```
camp unregister <name-or-id> [flags]
```

### Options

```
  -f, --force   Skip confirmation prompt
  -h, --help    help for unregister
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp version

Show version information

### Synopsis

Show camp version, build information, and runtime details.

When both --short and --json are provided, --json wins.

Examples:
  camp version           Show full version info
  camp version --short   Show only version number
  camp version --json    Output as JSON

```
camp version [flags]
```

### Options

```
  -h, --help    help for version
      --json    output as JSON
  -s, --short   show only version number
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workflow

Manage workflow collections

### Synopsis

Manage workflow collections.

A workflow collection is a campaign directory under workflow/<type>/ with
navigation config and workitem type support.

### Options

```
  -h, --help   help for workflow
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workflow create

Create a custom workflow collection

### Synopsis

Create a custom workflow collection under workflow/<type>/.

The command creates the workflow directory, terminal dungeon directories,
.gitkeep files, and an OBEY.md guide, then registers the collection in
campaign configuration through a concept and navigation shortcut. A shortcut is
required. Use --dry-run to inspect planned writes and --json for
machine-readable planning or apply results.

```
camp workflow create <type> [flags]
```

### Options

```
      --category string   workflow category for filtering (default plan; must exist under workflows.categories in campaign.yaml)
      --dry-run           report planned writes without modifying the filesystem
  -h, --help              help for create
      --json              emit a structured JSON result
      --replace           replace an existing shortcut or concept with the same name
      --shortcut string   navigation shortcut for this workflow
      --title string      human-readable workflow title
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workflow doctor

Report workflow surface inconsistencies

### Synopsis

Report inconsistencies between workflow directories and campaign configuration.

The command reads campaign.yaml, .campaign/settings/jumps.yaml, workflow/
directories, and the navigation cache to find missing concepts, stale
shortcuts, duplicate shortcut keys, and cache drift. Use --json for
machine-readable findings and stable finding codes.

```
camp workflow doctor [flags]
```

### Options

```
  -h, --help   help for doctor
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workflow list

List user-created workflow collections

### Synopsis

List user-created workflow collections registered in the campaign.

The command reads campaign configuration and workflow/ directories, then shows
each collection's shortcut, item count, and latest workitem update. Built-in
workflow types are omitted so the output focuses on custom collections. Use
--json for machine-readable workflow inventory output.

```
camp workflow list [flags]
```

### Options

```
  -h, --help   help for list
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workflow shortcut

Manage navigation shortcuts for workflow collections

### Synopsis

Manage navigation shortcuts for custom workflow collections.

Workflow shortcuts are stored in campaign configuration and point to
workflow/<type>/ directories. Use subcommands to attach or repair shortcut
entries after creating or moving workflow collections.

### Options

```
  -h, --help   help for shortcut
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workflow shortcut add

Attach a navigation shortcut to an existing workflow

### Synopsis

Attach a navigation shortcut to an existing workflow collection.

The command updates .campaign/settings/jumps.yaml so cgo and camp navigation
can jump to workflow/<type>/ by key. The workflow type must already exist. Use
--replace to overwrite a conflicting shortcut and --json for machine-readable
result details.

```
camp workflow shortcut add <type> <key> [flags]
```

### Options

```
  -h, --help      help for add
      --json      emit a structured JSON result
      --replace   replace an existing shortcut with the same name
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workflow show

Show a workflow collection's config and recent workitems

### Synopsis

Show configuration and recent workitems for a workflow collection.

The command reads campaign configuration plus the workflow/<type>/ directory,
then prints the collection path, shortcut state, concept state, and recent
.workitem-backed items. Use --json for machine-readable collection details and
recent workitem data.

```
camp workflow show <type> [flags]
```

### Options

```
  -h, --help   help for show
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workflow sync

Repair auto-fixable doctor findings

### Synopsis

Repair auto-fixable workflow findings reported by workflow doctor.

The command plans changes to campaign.yaml, .campaign/settings/jumps.yaml, and
the navigation cache for stale shortcuts, missing concepts, duplicate shortcut
keys, and cache drift. By default it reports the planned actions only; pass
--apply to write changes. Use --json for machine-readable plans and applied
actions.

```
camp workflow sync [flags]
```

### Options

```
      --apply   perform writes (default: report only)
  -h, --help    help for sync
      --json    emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem

View active campaign work items

### Synopsis

View active campaign work items.

Launches an interactive dashboard on a TTY. Non-interactive callers must pass
--json, --list, or --print.

Examples:
  camp workitem                       # interactive dashboard
  camp workitem --json --type design  # JSON, filtered by type
  camp workitem --list                # compact grouped list
  camp workitem --print               # print a path for shell integration

```
camp workitem [flags]
```

### Options

```
      --attention-stage stringArray   Filter by attention stage
      --category stringArray          Filter by workflow category
      --group stringArray             Filter by workitem group
      --group-by string               Group sections (default "attention_stage")
  -h, --help                          help for workitem
      --json                          Output as JSON
      --limit int                     Maximum items to return
      --list                          Output a compact grouped list
      --print                         Print path only
      --query string                  Filter by search query
      --show-parked                   Include parked workitems
      --stage stringArray             Filter by lifecycle stage
      --status stringArray            Filter by displayed status
      --type stringArray              Filter by workflow type
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem adopt

Adopt an existing directory as a workitem

### Synopsis

Attach workitem metadata to an existing campaign directory without moving it.

The target directory must already exist and must not already contain a
.workitem file. The command writes that .workitem metadata file with the
selected type, title, generated or supplied id, and optional quest link. Use
this when a workflow directory already exists and needs to become a tracked
workitem.

```
camp workitem adopt <dir> [flags]
```

### Options

```
  -h, --help           help for adopt
      --id string      override the generated id
      --quest string   quest ID to associate (requires dev-profile camp; forward-compatible flag)
      --title string   human-readable title
      --type string    workitem type (feature, bug, chore, or custom) (default "feature")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem commit

Commit changes scoped to a workitem

### Synopsis

Stage and commit changes belonging to a resolved workitem.

The staging plan is computed from the resolver context (cwd-aware, with
explicit positional <selector> or --project overrides) and printed to stderr
before the commit runs. The plan never silently widens to "git add ." at the
campaign root.

See docs/workitem-commit-reference.md for the staging matrix and flag
precedence.

```
camp workitem commit [selector] [flags]
```

### Options

```
      --dry-run                     print the staging plan and exit without committing
      --exclude stringArray         path to remove from the staging plan (repeatable)
      --festival string             festival id for the festival resolver tier
  -h, --help                        help for commit
      --include stringArray         additional path to stage (repeatable; relative to repo root)
      --include-submodule-pointer   include dirty project submodule pointers in the plan
      --json                        emit the staging plan and commit result as JSON on stdout
  -m, --message stringArray         commit message (repeatable; multiple -m are joined git-style into subject + body; required unless --dry-run)
      --project string              force project-repo context by name (skips resolver)
      --staged                      commit whatever is already in the git index
      --workitem string             explicit workitem selector (overrides cwd-based resolution)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem commits

List commits referencing a workitem

### Synopsis

List commits referencing this workitem, newest first.

When the campaign event ledger already holds the workitem's commit evidence,
the answer comes from a single merged ledger read (fast path). Otherwise it
falls back to scanning the campaign root and every linked
project/repo/worktree/festival repo for commits whose campaign tag references
the workitem's ref (pre-ledger history).

Use --json for structured output; the "source" field reports which path
answered ("ledger" or "scan"). Repos that are not git checkouts or that fail
their git log invocation are reported under "errors" in JSON mode; table mode
warns on stderr when repo queries fail.

```
camp workitem commits [selector] [flags]
```

### Options

```
  -h, --help              help for commits
      --json              emit JSON instead of the default table
      --limit int         maximum commits to return (default 100)
      --offset int        number of commits to skip (after sorting)
      --ref string        query by workitem ref directly (e.g. WI-abc123) — skips resolver
      --source string     where to read commits from: auto (ledger when present, else scan), ledger, or scan (default "auto")
      --workitem string   alias for the positional <selector>
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem create

Create a workitem

### Synopsis

Create a new workitem directory with minimal v1 metadata.

The workitem is created under workflow/<type>/<slug>/ unless --dir supplies a
different campaign-relative parent directory. A .workitem file is written with
the id, type, title, ref, creation metadata, and optional quest link. Use --json
for machine-readable output containing the new workitem identity and next-step
location.

```
camp workitem create <slug> [flags]
```

### Options

```
      --dir string     parent dir override (default: workflow/<type>)
  -h, --help           help for create
      --id string      override the generated id
      --json           emit a structured JSON result
      --quest string   quest ID to associate (requires dev-profile camp; forward-compatible flag)
      --title string   human-readable title
      --type string    workitem type (feature, bug, chore, or custom) (default "feature")
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem current

Get, set, or clear the current workitem

### Synopsis

Get, set, or clear the campaign-local current workitem pointer.

The selection is stored in .campaign/workitems/current.yaml and is used by
commands that need a default workitem when cwd alone is ambiguous. Pass a
selector to set the current workitem, omit it to read the selection, or use
--clear to remove it. Use --json for machine-readable current selection output.

```
camp workitem current [selector] [flags]
```

### Options

```
      --clear   remove the local current.yaml selection
  -h, --help    help for current
      --json    emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem doctor

Report link-registry health issues

### Synopsis

Report health issues in the campaign workitem link registry.

The command reads .campaign/workitems/links.yaml, scans .workitem metadata on
disk, and checks current-workitem and priority stores for stale or inconsistent
references. Use --fix to apply auto-repairs for supported findings. Use --json
for machine-readable findings and stable finding codes.

```
camp workitem doctor [flags]
```

### Options

```
      --fix    auto-repair findings tagged auto_fixable
  -h, --help   help for doctor
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem group

Set or clear the group

```
camp workitem group <selector> <group|clear> [flags]
```

### Options

```
  -h, --help   help for group
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem link

Create a workitem link

### Synopsis

Attach a workitem to a project, festival, worktree, or campaign path.

Links are stored in .campaign/workitems/links.yaml and connect a .workitem
identity to an explicit scope for planning, execution, and lookup. Pass a
workitem selector plus a path, or use --project, --festival, --worktree, or
--cwd to derive the scope. Use --json for machine-readable link output.

A primary worktree link is how design/explore workitems under workflow/ get
into camp p commit tags: when you commit from that worktree, the resolver
matches the link and stamps WI-<ref> on the subject.

Examples:
  camp workitem link WI-2a7950 --worktree fest/fest-list-watch
  camp workitem link workflow/design/fest-list-watch --worktree projects/worktrees/fest/fest-list-watch
  camp workitem link WI-2a7950 projects/worktrees/fest/fest-list-watch
  # Or at create time:
  camp project worktree add fest-list-watch --project fest --workitem WI-2a7950

```
camp workitem link <selector> [path] [flags]
```

### Options

```
      --allow-missing     allow the workitem and scope target to not exist (migrations)
      --cwd               use current working directory as the scope
      --festival string   festival id or relative path under festivals/
  -h, --help              help for link
      --json              emit a structured JSON result
      --project string    project name (matches projects/<name>)
      --replace           replace an existing primary link on the same scope
      --role string       primary | related | blocked_by | supersedes (default "primary")
      --worktree string   worktree path under projects/worktrees/ (project/name or full projects/worktrees/project/name)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem links

List workitem links

### Synopsis

List workitem links recorded in the campaign link registry.

The command reads .campaign/workitems/links.yaml and prints every link, or only
links for the supplied workitem selector. Use this to audit which projects,
festivals, worktrees, or paths are attached to a workitem. Use --json for
machine-readable link lists.

```
camp workitem links [selector] [flags]
```

### Options

```
  -h, --help   help for links
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem list

List or browse filtered workitems

### Synopsis

List campaign workitems with the same filters used by the dashboard.

In a terminal, this opens the TUI with visible, editable prefilters. When
stdout is not a terminal, it prints a compact grouped list. Use --json for the
stable machine-readable contract in either environment.

The optional positional filter resolves as a workflow type, displayed status,
or configured category. Ambiguous values must use an explicit flag.

Examples:
  camp workitem list intent
  camp workitem list active
  camp workitem list --category research --query auth
  camp workitem list festival --status ready --json

```
camp workitem list [type|status|category] [flags]
```

### Options

```
      --attention-stage stringArray   Filter by attention stage (repeat for OR)
      --category stringArray          Filter by workflow category (repeat for OR)
      --group stringArray             Filter by workitem group (repeat for OR)
      --group-by string               Group output sections by attention_stage, group, type, or category
  -h, --help                          help for list
      --json                          Output as JSON
      --limit int                     Maximum number of items to return (non-interactive / --json only)
      --query string                  Search query to filter items
      --show-parked                   Include parked attention-stage workitems
      --stage stringArray             Filter by lifecycle stage (repeat for OR)
      --status stringArray            Filter by displayed status: current, next, active, parked, inbox, ready, plan, ritual, chains, none (repeat for OR)
      --type stringArray              Filter by workflow type (repeat for OR)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem priority

Set or clear the manual priority

### Synopsis

Set or clear the manual priority of a workitem.

The selector accepts the same forms as 'camp workitem current': a stable
.workitem id, the workitem key (<type>:<path>), a relative path, or a directory
slug. Priority is one of high, medium, low, or clear (clear removes any manual
priority). Assignments persist in .campaign/settings/workitems.json, the same
store the interactive dashboard writes.

Examples:
  camp workitem priority festival:festivals/active/demo high
  camp workitem priority demo clear
  camp workitem priority demo high --json

```
camp workitem priority <selector> <high|medium|low|clear> [flags]
```

### Options

```
  -h, --help   help for priority
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem promote

Promote a workitem to a festival, doc, or dungeon

### Synopsis

Promote the workitem identified by [id], by cwd, or by the current pointer.

TARGETS:
  festival    Create a festival from the workitem and shelve the source
  doc         Copy the workitem doc into docs/ and shelve the source
  completed   Move the workitem to its local dungeon/completed
  archived    Move the workitem to its local dungeon/archived
  someday     Move the workitem to its local dungeon/someday

```
camp workitem promote [id] --target <target> [flags]
```

### Options

```
      --dest string     Destination path under docs/ for the doc target (must stay within docs/)
      --dry-run         Print the planned action, change nothing
      --force           Skip readiness checks (e.g. empty doc)
      --goal string     Festival goal override (default: first paragraph of the workitem doc)
  -h, --help            help for promote
      --json            Output result as a single JSON object
      --keep            On festival/doc, do not move the source workitem to the dungeon
      --no-commit       Skip the auto-commit
      --target string   Promotion target: festival, doc, completed, archived, someday
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem repair

Repair a workflow directory into a workitem

### Synopsis

Repair a workflow directory so it carries a valid current-schema .workitem marker.

The directory is never moved or renamed and document contents are never touched.
When no marker exists one is created; when a legacy or incomplete marker exists
its schema version, kind, id, type, ref, and title are brought up to the current
shape. The workflow type is inferred from the path segment after workflow/, the
title from the first markdown H1 (else the humanized directory name), and id/ref
from the same rules as create and adopt. Repair is idempotent: a directory that
is already valid reports no changes. Use --dry-run to preview and --json for a
machine-readable result.

```
camp workitem repair <path> [flags]
```

### Options

```
      --dry-run       report what would change without writing
  -h, --help          help for repair
      --json          emit a structured JSON result
      --type string   override the workflow type inferred from the path
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem resolve

Print the workitem for the current context

### Synopsis

Resolve the active workitem from the current campaign context.

Resolution checks explicit selectors, cwd, festival context, linked scopes,
and the current-workitem file without mutating any files. Use --explain to show
the tier-by-tier trace used to choose the result. Use --json for
machine-readable resolution details and trace data.

```
camp workitem resolve [flags]
```

### Options

```
      --explain           print the tier-by-tier resolution trace
      --festival string   festival id for the festival tier
  -h, --help              help for resolve
      --json              emit a structured JSON result
      --workitem string   explicit workitem selector (overrides cwd-based detection)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem stage

Set or clear the attention stage

```
camp workitem stage <selector> <current|next|active|parked|clear> [flags]
```

### Options

```
  -h, --help   help for stage
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem unlink

Remove workitem links

### Synopsis

Remove workitem links from the campaign link registry.

The command updates .campaign/workitems/links.yaml by link id, workitem
selector, explicit path, or scope filter. Use --all when a selector matches
multiple links and every match should be removed. Use --json for
machine-readable details about the removed links.

```
camp workitem unlink [selector] [path] [flags]
```

### Options

```
      --all               remove every link matching the selector
      --festival string   festival scope filter
  -h, --help              help for unlink
      --id string         remove the link with this lnk_ id
      --json              emit a structured JSON result
      --project string    project scope filter
      --worktree string   worktree scope filter
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem validate

Validate workitem directories

### Synopsis

Validate that workflow work item directories carry a correct .workitem marker.

Without an argument, every work item directory under workflow/ is scanned:
builtin doc directories (workflow/design, workflow/explore) are always work
items, custom type directories surface only when they carry a marker, and
dungeon/hidden control areas are ignored. With a path argument, only that
directory is validated.

Each problem prints the exact repair command, for example
"camp workitem repair workflow/design/foo". Use --json for stable finding
codes. The command exits non-zero when any error-severity finding is present.

```
camp workitem validate [path] [flags]
```

### Options

```
  -h, --help   help for validate
      --json   emit a structured JSON result
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```
---

## camp workitem worktree

Create a project worktree from a workitem

### Synopsis

Create a git worktree for a workitem and primary-link it, so commits in
that worktree carry the workitem's WI-* tag.

This is the workitem-first counterpart to 'camp project worktree add': instead
of naming a worktree and optionally tagging a workitem, you name a workitem and
the worktree name, branch, and link are derived from it.

Project resolution:
  The target project is taken from the workitem's linked project (see
  'camp workitem link --project'). When the workitem has no project link, or
  is linked to more than one, pass --project explicitly.

Re-entry:
  If the workitem already has a primary worktree link, the existing path is
  printed and no new worktree is created.

Examples:
  # Festival workitem already linked to a project
  camp workitem worktree WI-2a7950

  # Design/explore/intent workitem: name the project
  camp workitem worktree workflow/design/camp-settings-tui --project camp

  # Override the derived worktree name
  camp workitem worktree WI-2a7950 --name grok-list-fix

  # Print only the path (for shell integration)
  cd "$(camp workitem worktree WI-2a7950 --print)"

```
camp workitem worktree <selector> [flags]
```

### Options

```
  -h, --help                 help for worktree
      --name string          Worktree/branch name (derived from the workitem if omitted)
      --print                Print only the worktree path
  -p, --project string       Project name (inferred from the workitem's project link if omitted)
  -s, --start-point string   Base branch/commit for the new branch (default: current branch)
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```