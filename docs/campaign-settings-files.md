# Campaign Settings Files

Camp uses two scopes of configuration:

- Global user preferences in `~/.obey/campaign/`
- Campaign-local files under `.campaign/`

This guide documents the settings and state files users are most likely to
see, what creates them, and whether they are intended for direct editing.

## Quick Reference

| File | Scope | Format | Created by | Edit directly? |
| --- | --- | --- | --- | --- |
| `~/.obey/campaign/config.json` | Global | JSON | Auto-created on first load or via `camp settings` | Yes |
| `~/.obey/campaign/registry.json` | Global | JSON | `camp register`, `camp list`, `camp switch` flows | Usually no |
| `.campaign/campaign.yaml` | Campaign | YAML | `camp init` / `camp init --repair` | Yes |
| `.campaign/settings/jumps.yaml` | Campaign | YAML | `camp init`, `camp init --repair`, or auto-created on load if missing | Yes |
| `.campaign/settings/fresh.yaml` | Campaign | YAML | `camp init` / `camp init --repair` | Yes |
| `.campaign/settings/pins.json` | Campaign | JSON | `camp pin` / `camp unpin` | Usually no |
| `.campaign/settings/allowlist.json` | Campaign | JSON | `camp init`, `camp init --repair`, or tooling that saves allowlist config | Sometimes |
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

Files like `.campaign/settings/pins.json` and `.campaign/leverage/` are created
later when the corresponding feature is used.

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
- `watchers.yaml`, `pins.json`, and most quest/runtime state are tool-managed
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
  }
}
```

Notes:

- If the file does not exist, camp loads defaults and tries to create it.
- `editor` is only used if `$EDITOR` and `$VISUAL` are not set.
- `camp settings` currently supports the global settings in this file.

### `~/.obey/campaign/registry.json`

The registry tracks known campaigns for commands like `camp list` and
`camp switch`. It is camp-managed state rather than a normal hand-authored
settings file.

You usually should not edit it manually unless you are repairing a broken
registry entry.

## Campaign Files

### `.campaign/campaign.yaml`

The main campaign metadata file.

It stores:

- Campaign identity: `id`, `name`, `type`
- Human context: `description`, `mission`
- Project entries under `projects`
- Picker concepts under `concepts`

Navigation paths and shortcuts do not live here anymore; those live in
`.campaign/settings/jumps.yaml`.

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

## `camp settings` Status

`camp settings` is only a partial configuration editor today:

- Global settings are implemented and saved to `~/.obey/campaign/config.json`
- Local campaign settings in the TUI are still a scaffold and do not write the
  files under `.campaign/settings/`

For now, campaign-local configuration is file-based. Edit the relevant file
directly when you need to customize `jumps.yaml`, `fresh.yaml`, or
`allowlist.json`.
