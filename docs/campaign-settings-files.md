# Campaign Settings Files

Camp uses two scopes of configuration:

- Global user preferences in `~/.obey/campaign/`
- Campaign-local files under `.campaign/`

This guide documents the settings and state files users are most likely to
see, what creates them, and whether they are intended for direct editing.

For the full `.campaign/` directory layout, including `quests/`, `skills/`,
`cache/`, and `leverage/`, see
[campaign-directory-reference.md](campaign-directory-reference.md).

## Quick Reference

| File | Scope | Format | Created by | Edit directly? |
| --- | --- | --- | --- | --- |
| `~/.obey/campaign/config.json` | Global | JSON | Auto-created on first load or via `camp settings` | Yes, or via `camp settings` |
| `~/.obey/campaign/registry.json` | Global | JSON | `camp register`, `camp list`, `camp switch` flows | Safe edits via `camp settings` |
| `.campaign/campaign.yaml` | Campaign | YAML | `camp init` / `camp init --repair` | Yes, or via `camp settings` |
| `.campaign/settings/jumps.yaml` | Campaign | YAML | `camp init`, `camp init --repair`, or auto-created on load if missing | Yes |
| `.campaign/settings/fresh.yaml` | Campaign | YAML | `camp init` / `camp init --repair` | Yes |
| `.campaign/settings/pins.json` | Campaign | JSON | `camp pin` / `camp unpin` | Usually no |
| `.campaign/settings/allowlist.json` | Campaign | JSON | `camp init`, `camp init --repair`, or `camp settings` | Yes, or via `camp settings` |
| `.campaign/settings/local.json` | Campaign | JSON | `camp settings` (Local Settings) or `camp settings set local.*` | Usually no |
| `.campaign/watchers.yaml` | Campaign | YAML | `camp init`, `camp init --repair`, `fest`, contract writers | No |

## What `camp init` And `camp init --repair` Create

Today, `camp init` and `camp init --repair` reliably scaffold:

- `.campaign/.gitignore`
- `.campaign/campaign.yaml`
- `.campaign/intents/`
- `.campaign/quests/`
- `.campaign/settings/fresh.yaml`
- `.campaign/settings/jumps.yaml`
- `.campaign/settings/allowlist.json`
- `.campaign/skills/`
- `.campaign/watchers.yaml`

They do not currently scaffold:

- `.campaign/settings/pins.json`
- `.campaign/settings/local.json`

Files like `.campaign/settings/pins.json`, `.campaign/settings/local.json`,
and `.campaign/leverage/` are created later when the corresponding feature is
used.

## Typical `.campaign/` Layout

After `camp init`, the hidden campaign directory typically looks like this:

```text
.campaign/
├── .gitignore
├── campaign.yaml
├── intents/
├── quests/
├── settings/
│   ├── allowlist.json
│   ├── fresh.yaml
│   └── jumps.yaml
├── skills/
└── watchers.yaml
```

Not everything under `.campaign/` is the same kind of file:

- `campaign.yaml`, `settings/fresh.yaml`, and `settings/jumps.yaml` are normal
  user-editable configuration
- `watchers.yaml`, `pins.json`, `settings/local.json`, and most quest/runtime
  state are tool-managed
- `skills/` is campaign content that camp scaffolds and skill-related commands
  consume
- `leverage/` appears later when leverage commands write config or snapshots

## Global Files

### `~/.obey/campaign/config.json`

Global user preferences for camp. This is the file that `camp settings`
actually edits today.

Current fields:

```json
{
  "editor": "nvim",
  "no_color": false,
  "verbose": false,
  "tui": {
    "theme": "adaptive",
    "vim_mode": false
  },
  "campaigns_dir": "~/campaigns/",
  "commit": {
    "sync_project_refs": false,
    "disable_commit_tags": false
  }
}
```

Notes:

- If the file does not exist, camp loads defaults and tries to create it.
- `editor` is only used if `$EDITOR` and `$VISUAL` are not set.
- `commit.sync_project_refs` / `commit.disable_commit_tags` are machine-wide
  defaults for project-ref linking after `camp p commit` and for campaign
  subject-tag tracing. Per-campaign overrides live in
  `.campaign/settings/local.json` under the same `commit` object.
- `camp settings` currently supports the global settings in this file.

#### `campaigns_dir`

The directory where `camp create` places new campaigns.

- **Default**: `~/campaigns/` (used when the field is absent or empty).
- **Accepted formats**: absolute path (e.g. `/home/you/work/campaigns`), tilde-prefixed path (e.g. `~/work/campaigns`), or a path relative to `$HOME` (discouraged but allowed).
- **Resolution**: tilde and relative paths are expanded to an absolute path at runtime. The config file always stores the portable, unexpanded form.
- The directory does not need to exist in advance. `camp create` creates it on the first run.
- To discover or change this setting interactively, run `camp settings` and choose Global Settings. The "Campaigns Dir" row edits this field. Leaving it blank resets to the default `~/campaigns/`.

### `~/.obey/campaign/registry.json`

The registry tracks known campaigns for commands like `camp list` and
`camp switch`. It is camp-managed state rather than a normal hand-authored
settings file.

You usually should not edit it manually. For the common repair cases,
`camp settings` (Global Settings, then Campaign registry) offers safe
per-campaign edits - org assignment, display rename, and path repair - written
through the registry API so the format and other entries stay intact. Lifecycle
operations (register, unregister, switch, transfer) remain dedicated commands.

## Campaign Files

### `.campaign/campaign.yaml`

The main campaign metadata file.

It stores:

- Campaign identity: `id`, `name`, `type`
- Human context: `description`, `mission`
- Project entries under `projects`
- Picker concepts under `concepts`
- Workflow categories under `workflows`

Navigation paths and shortcuts do not live here anymore; those live in
`.campaign/settings/jumps.yaml`.

`camp workflow create <type>` adds a concept entry under `concepts:` for
the workflow type. The entry name is the workflow type slug and the path
points to `workflow/<type>/`. Use `--replace` to overwrite a concept entry
that already exists under the same name.

#### `workflows` (categories)

The `workflows` block classifies workflow/workitem types into broad categories
so `camp workitem` can filter and group by kind of work. It is written by
`camp init` and backfilled by `camp init --repair` (repair preserves your edits).

```yaml
workflows:
  categories:
    plan:
      label: Plan
      description: Planning, design, intents, festivals, and structured execution work
    research:
      label: Research
    pipeline:
      label: Pipeline
    review:
      label: Review
  category_by_type:
    intent: plan
    design: plan
    explore: research
    festival: plan
    code_reviews: review
    pipelines: pipeline
```

- `categories` defines the vocabulary and display metadata. The shipped set is
  `plan`, `research`, `pipeline`, `review`; add your own as needed.
- `category_by_type` maps a workflow/workitem type key to a category key. Any
  slug-safe type may be mapped, even before a matching item exists. Types with
  no mapping resolve to `uncategorized` and still appear in listings.
- `camp workflow create <type> --category <cat>` writes the mapping for you; the
  category must already exist under `categories`.
- Category is derived at read time; it is never stored in `.workitem` files, so
  changing a mapping reclassifies every item of that type at once.

### `.campaign/settings/jumps.yaml`

Campaign-local navigation settings.

It contains:

- `paths`: the canonical directory locations camp uses for navigation
- `shortcuts`: custom or overridden `camp go`, `cgo`, and `camp run` entries

Example:

```yaml
paths:
  projects: projects/
  festivals: festivals/
  intents: .campaign/intents/

shortcuts:
  api:
    path: projects/api-service
    description: Jump to the API service

  test:
    command: just test
    workdir: projects/camp
    description: Run camp tests
```

Notes:

- `camp init` writes the default file.
- If `jumps.yaml` is missing, loading campaign config will recreate default
  jumps in memory and try to save the file back to disk.
- `camp init --repair` preserves user-defined shortcuts and adds missing
  built-in shortcuts.
- `camp workflow create <type>` adds a shortcut entry under `shortcuts:` for
  the workflow type. The default key is the workflow type slug and the path
  points to `workflow/<type>/`. Use `--shortcut <key>` to override the key
  and `--replace` to overwrite an existing shortcut at the same key.
- `camp workflow shortcut add` and `camp workflow sync` write shortcut
  entries the same way and target the same file.

See [SHORTCUTS.md](SHORTCUTS.md) for shortcut-specific details.

### `.campaign/settings/fresh.yaml`

Optional defaults for `camp fresh`.

Example:

```yaml
branch: lr/develop
push_upstream: true
prune: true
prune_remote: true
projects:
  camp:
    branch: feat/camp
    push_upstream: false
```

Behavior:

- `branch` sets the default branch to create after sync.
- `push_upstream` controls whether the new branch is pushed with
  `--set-upstream`.
- `prune` defaults to `true` when omitted.
- `prune_remote` defaults to `true` when omitted.
- `projects.<name>.branch` overrides the top-level `branch` for one project.
- `projects.<name>.push_upstream` overrides the top-level `push_upstream` for
  one project.
- `camp init` scaffolds this file with commented examples and field guidance.

Branch resolution order:

1. `camp fresh --no-branch`
2. `camp fresh --branch ...`
3. `projects.<name>.branch`
4. Top-level `branch`
5. No branch creation

Current limitations:

- The branch name is literal text. There is no templating from git user name
  or initials yet.
- Camp only creates the branch if it does not already exist locally.

### `.campaign/settings/pins.json`

Camp stores navigation pins here.

Example:

```json
[
  {
    "name": "camp",
    "path": "projects/camp",
    "created_at": "2026-03-18T18:24:31Z"
  }
]
```

Notes:

- This file is created on first save, usually by `camp pin`.
- Camp migrates legacy `.campaign/pins.json` data into this location.
- Relative paths inside the campaign are preferred and may be normalized on
  load/save flows.

### `.campaign/settings/allowlist.json`

Optional daemon command allowlist overrides for a specific campaign.

Example:

```json
{
  "version": 1,
  "inherit_defaults": true,
  "commands": {
    "camp": {
      "allowed": true,
      "description": "Campaign CLI"
    },
    "just": {
      "allowed": true,
      "description": "Task runner"
    }
  }
}
```

Notes:

- `camp init` scaffolds a starter allowlist file with the current default
  command set.
- If the file is missing, callers are expected to use default allowlist
  behavior.
- `inherit_defaults: true` means the campaign file extends the daemon defaults.
- `inherit_defaults: false` means only commands listed in this file are
  explicitly allowed.
- `camp settings` (Local Settings, then Command allowlist) edits this file:
  toggle `allowed` per command, add, and remove commands. `inherit_defaults` is
  shown there but only changed by hand-editing.

### `.campaign/settings/local.json`

Campaign-local settings managed by `camp settings`.

Example:

```json
{
  "theme_override": "dark",
  "commit": {
    "sync_project_refs": false,
    "disable_commit_tags": false
  }
}
```

Notes:

- `theme_override` forces a color theme for this campaign: one of `adaptive`,
  `light`, `dark`, or `high-contrast`. When absent or empty, the campaign
  inherits the global theme from `~/.obey/campaign/config.json`.
- `commit` (optional) fully overrides machine-global commit prefs for this
  campaign when present:
  - `sync_project_refs` — when true, `camp project commit` updates the
    campaign-root submodule pointer after a project commit (same as `--sync`).
    Default is false. Use `--no-sync` on a single invocation to force off.
  - `disable_commit_tags` — when true, skips `[campaign:id-…]` subject prefixes
    on camp-managed commits. Default is false (tags enabled).
- The file is created on first save by `camp settings` (Local Settings) or
  `camp settings set local.*`. Clearing the last setting deletes the file.
- Prefer the `camp settings` commands over hand-editing; writes are atomic and
  lock-protected.

### `.campaign/watchers.yaml`

The campaign watcher contract. This is camp/fest-owned machine-readable state
used by the daemon to know which files and directories to watch.

It contains entries such as:

- `.campaign/campaign.yaml`
- `.campaign/settings/jumps.yaml`
- `.campaign/settings/pins.json`
- `.campaign/settings/allowlist.json`
- `.campaign/intents/*` status directories

Important:

- This file is merge-safe across tools because entries are owner-scoped.
- `camp init --repair` can regenerate camp-owned entries.
- Users generally should not hand-edit it unless they are debugging contract
  behavior.

## `camp settings` Scope

`camp settings` is a catalog-driven, path-transparent editor for both
configuration scopes. It presents one row per settings file and, on every
screen, shows the exact file it edits: campaign-root-relative for local files
(for example `.campaign/campaign.yaml`) and tilde-based for global files (for
example `~/.obey/campaign/registry.json`). Editing through the TUI is a guided,
safer version of hand-editing the same files.

### What it edits

Local (under `.campaign/`):

- `campaign.yaml` - identity, mission, and type via a structured form; the
  `intents.tags` list via a one-per-line editor; and the nested `concepts`
  taxonomy via a `$EDITOR` round-trip (the single explicit editor exception,
  validated all-or-nothing so an invalid edit never touches the file).
- `settings/local.json` - the campaign theme override and optional commit
  behavior overrides (project-ref sync, disable commit tags).
- `settings/allowlist.json` - toggle `allowed` per command, add, and remove
  commands. `inherit_defaults` is shown but not changed here.

Global (under `~/.obey/campaign/`):

- `config.json` - theme, editor, campaigns dir, verbose, no-color, and commit
  defaults (project-ref sync, disable commit tags).
- `registry.json` - view registered campaigns and make safe per-campaign edits
  (org, display name, and path repair). Path repair only points at an existing
  directory and asks for confirmation. Lifecycle operations (register,
  unregister, switch, transfer) stay in `camp registry` / `camp org`.

Machine-managed files (leverage config, pins, jumps, watcher state, and the
like) are marked hidden in the catalog and never appear in the menu; secrets
such as `~/.obey/.env` are never listed or read. See "Extending the settings
surface" below.

### Non-interactive access

For agents, scripts, and the festival app, the `get`/`set` twin mirrors the TUI
for values that map to a single flat key:

```bash
camp settings get                          # Aggregate view plus the effective theme
camp settings get global.theme             # One value
camp settings get local.campaign.mission   # One campaign.yaml scalar
camp settings get --json                   # Versioned JSON payload
camp settings set global.theme dark
camp settings set local.theme_override light
camp settings set local.theme_override inherit    # Clear the override
camp settings set local.campaign.type research
camp settings set global.commit.disable_commit_tags true
camp settings set local.commit.sync_project_refs true
```

Keys: `global.theme`, `global.editor`, `global.campaigns_dir`,
`global.verbose`, `global.no_color`, `global.commit.sync_project_refs`,
`global.commit.disable_commit_tags`, `local.theme_override`,
`local.commit.sync_project_refs`, `local.commit.disable_commit_tags`,
`local.campaign.name`, `local.campaign.description`, `local.campaign.mission`,
`local.campaign.type`, and `local.campaign.commit_hook`. The `local.*` keys
require running inside a campaign. The campaign.yaml list and tree fields
(`intents.tags`, `concepts`) and the registry per-campaign edits have no flat
key and are edited only through the interactive TUI.

Other campaign-local files (`jumps.yaml`, `fresh.yaml`) remain file-based; edit
the relevant file directly when you need to customize them.

### Extending the settings surface

The settings menu is generated from an in-code catalog (`internal/settings`),
so adding a file is data, not menu code:

- A **structured** entry needs one catalog line plus a hand-authored form; that
  form is the irreducible part.
- A **read-only** or **hidden** entry needs only a catalog line, and no
  `settings.go` change.
- The **hidden** set is derived from the watcher contract
  (`internal/contract`), so any newly-watched camp-managed file is excluded
  from the menu automatically.
- **Secret** files are hard-coded as never listed and never read.
