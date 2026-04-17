# `.campaign/` Directory Reference

`.campaign/` is camp's hidden campaign metadata directory. It mixes three
kinds of content:

- user-editable configuration
- camp/fest-managed shared state
- machine-local cache and history

This page documents the top-level layout so users can tell which parts are
safe to edit and which parts are runtime-owned.

## Typical Layout

After `camp init`, the directory usually looks like this:

```text
.campaign/
├── .gitignore
├── campaign.yaml
├── watchers.yaml
├── intents/
│   ├── OBEY.md
│   ├── inbox/
│   ├── ready/
│   ├── active/
│   └── dungeon/
├── quests/
│   ├── default/
│   │   └── quest.yaml
│   └── dungeon/
├── settings/
│   ├── allowlist.json
│   ├── fresh.yaml
│   └── jumps.yaml
├── skills/
│   ├── camp-navigation/
│   ├── camp-projects/
│   └── ...
├── cache/                # created later
│   ├── nav-index.json
│   └── state.jsonl
├── activity/             # created later by plugins such as camp-activity
│   └── ...
└── leverage/             # created later
    ├── config.json
    └── snapshots/
```

Notes:

- `settings/pins.json` is created later on first pin, so it is not shown in the
  initial scaffold.
- `cache/`, `activity/`, and `leverage/` are feature-created directories, not
  initial scaffold directories.
- `watchers.yaml` is written by contract-aware tools such as camp and fest.

## What You Can Edit

Normal user-edited configuration:

- `.campaign/campaign.yaml`
- `.campaign/settings/jumps.yaml`
- `.campaign/settings/fresh.yaml`
- `.campaign/settings/allowlist.json`

Usually tool-managed; avoid hand-editing unless you are debugging:

- `.campaign/watchers.yaml`
- `.campaign/settings/pins.json`
- `.campaign/cache/`
- `.campaign/activity/`
- `.campaign/leverage/snapshots/`
- most files under `.campaign/intents/` and `.campaign/quests/`

## Top-Level Entries

### `.campaign/.gitignore`

Camp scaffolds a local `.gitignore` here to keep machine-local runtime state
out of git. It excludes items such as `state.yaml`, `cache/`, and
`activity/`.

### `.campaign/campaign.yaml`

The primary campaign metadata file. It stores campaign identity, human context,
project entries, and concept configuration.

See [campaign-settings-files.md](campaign-settings-files.md) for the field-level
details.

### `.campaign/watchers.yaml`

The watcher contract shared by camp and fest. It tells the daemon which files
and directories to watch and which tool owns each entry.

This is camp/fest-managed state, not a general-purpose user config file.

### `.campaign/intents/`

System-managed intent state used by `camp intent` and `cgo i`.

The canonical status buckets are:

- `inbox/`
- `ready/`
- `active/`
- `dungeon/`

Camp repairs legacy `workflow/intents/` layouts into this canonical location.

### `.campaign/quests/`

Quest working contexts live here. The default scaffold includes
`default/quest.yaml`, and additional quest lifecycle state may appear under the
quest dungeon.

Important:

- quests are long-lived working contexts, not the same thing as festivals
- there is no required `.active` file; multiple quests can exist simultaneously

### `.campaign/settings/`

Campaign-local settings and small JSON/YAML state files live here.

Scaffolded by `camp init`:

- `allowlist.json`
- `fresh.yaml`
- `jumps.yaml`

Created later:

- `pins.json`

See [campaign-settings-files.md](campaign-settings-files.md) for the file-by-file
settings reference.

### `.campaign/skills/`

The campaign-local source of truth for skill bundles. `camp skills` projects
these bundles into provider/tool-specific locations such as `.claude/skills/`
or `.agents/skills/`.

This directory is scaffolded so campaigns start with baseline navigation and
workflow skills.

### `.campaign/cache/`

Machine-local runtime cache. Camp creates this when features need it.

Known files:

- `nav-index.json` for cached navigation index data
- `state.jsonl` for recent navigation history

This directory should be treated as local runtime state, not shared campaign
configuration.

### `.campaign/leverage/`

Created when leverage commands are used.

Known files:

- `config.json` for leverage configuration
- `snapshots/` for historical leverage snapshots

See [leverage-score.md](leverage-score.md) for leverage-specific details.

### `.campaign/activity/`

Plugin-managed activity cache and local report state. This directory is
reserved for tools such as `camp-activity` and should be treated as
machine-local runtime data, not shared configuration.

## Regeneration Safety

This page is a hand-written document in `docs/`, so `just docs` will not
overwrite it.

Generated CLI reference output lives under `docs/cli-reference/` and is safe to
regenerate because it is produced from command help text.
