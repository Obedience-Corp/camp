# Camp Terminology Contract

This is the authoritative vocabulary contract for Camp and everything built on
it: Fest, Festival, the Festival installer and app, Camp extensions, Obey
surfaces, generated skills, and public sites.

It exists so independent contributors and agents can make consistent wording
changes without re-deriving scope. If a wording question is not answered here,
answer it the way this page would and then extend this page.

Status: binding. Supersedes any older wording guidance in this repository.

## What a camp is

You have different camps in your life: your job, your side projects, your
taxes. Each of these has its own context, its own group of projects and
workflows you care about. A camp is that context.

Camp is the tool that manages your camps. Inside a camp you keep projects and
you run festivals, and a festival is where planned work actually gets executed.

```text
Camp manages camps
  -> camps contain projects
  -> camps host festivals
  -> festivals contain phases, sequences, and tasks
```

"Campaign" was the earlier name for a camp. It lost people. It read as jargon
and needed explaining every time. "Camp" is shorter, it lands immediately, and
it fits the product line now that the suite is called Festival.

Use this story, or a shortened version of it, as the canonical definition copy
in help text, onboarding, READMEs, and docs. Do not invent a competing
explanation.

## The five terms

| Term | Meaning | Example |
| --- | --- | --- |
| **Camp** | The product and the CLI. Capital C. | "Camp manages your camps." |
| **camp** | One managed workspace. Lowercase c, a common noun. | "Run `camp init` to create a camp." |
| **camps** | More than one managed workspace. | "Switch between camps with `csw`." |
| **workspace** | Generic clarifier, used only when "camp" alone would be ambiguous. | "the camp workspace root" |
| **campaign** | The stable technical name that survives in paths, flags, schemas, wire formats, database identifiers, code, and historical data. Also a living user synonym for camp. | "`.campaign/`", "`campaign_root`" |

There is no sixth term. Do not introduce "camp workspace" as a distinct
entity, "campsite", "encampment", "camp project", or any other variant.

## Capitalization

1. **Camp** with a capital C only when you mean the product or the CLI as a
   named thing. "Camp reads the registry." "Camp and Fest ship together."
2. **camp** lowercase everywhere it is a common noun, including at the start of
   a sentence where a capital is grammatically required. Prefer rewriting the
   sentence over capitalizing the noun: write "Your camp was not found" rather
   than "Camp was not found", which reads as the product.
3. In **headings and titles**, follow the surrounding document's existing case
   style. Do not capitalize the common noun just because it sits in a heading:
   "Working with camps", not "Working With Camps" in a sentence-case document.
4. In **UI labels and table headers**, capitalize as a label, not as a product:
   "Camps", "Active camp", "Camp root". A label capital does not turn the
   common noun into the product name.
5. The literal command name `camp` is always lowercase and always in code
   formatting when it is something the user types.
6. **campaign** in prose is lowercase. Technical spellings keep their exact
   case: `.campaign/`, `CAMP_ROOT`, `OBEY_CAMPAIGN_ID`, `campaign_root`,
   `CampaignConfig`.

## Singular and plural

- One workspace is "a camp". Never "a campaign" in new product copy, and never
  "a Camp".
- Many are "camps". The registry holds "registered camps".
- Possessive: "your camp", "the camp's projects". Avoid "Camp's" unless you
  genuinely mean the product's own property, as in "Camp's exit codes".
- "Camp manages camps" is accepted and intentional. If a specific sentence
  reads as repetitive, use the generic clarifier once: "Camp manages your
  workspaces, which the product calls camps." Do not fix repetition by
  reintroducing "campaign" as the entity noun.

## Disambiguation

These are five different things. Never let a sentence blur two of them.

| Thing | What it is | How to refer to it |
| --- | --- | --- |
| **Camp** | The product and the `camp` binary. | "Camp", "the `camp` CLI" |
| **a camp** | One managed workspace on disk, containing projects and festivals. | "a camp", "your camp", "the camp root" |
| **a festival** | A planned unit of work hosted inside a camp, containing phases, sequences, and tasks. Managed by Fest, not by Camp. | "a festival" |
| **`.campaign/`** | The hidden metadata directory at the camp root. A frozen technical path, not a name users should ever change. | "the camp metadata directory, `.campaign/`" |
| **`.camp`** | The attachment marker file written into a linked external directory so commands there can recover camp context. Not a directory, not the metadata store. | "the `.camp` attachment marker" |

Two mistakes to actively prevent, because they cause real damage:

- **`.camp` is not the new `.campaign/`.** It is an existing marker file with a
  different job. Never write copy that implies users should rename or migrate
  `.campaign/` to `.camp/`. Where a user might reasonably guess this, say
  plainly that they should not.
- **A camp is not a festival.** A camp is the long-lived context; a festival is
  a unit of planned work inside it. Do not use them interchangeably even
  loosely.

When a paragraph mixes the product and the entity in a way that could confuse a
first-time reader, use the clarifier: "the camp workspace root", "your camp
workspace". Prefer this in technical documentation, error text, and API
reference. Marketing and onboarding copy should stay with the plain noun.

## Campaign is a living user synonym

"Campaign" is not internal-only plumbing. Real users learned it, and some will
keep using it. Beyond the frozen technical boundary below, campaign keeps
working as user vocabulary.

1. **Agents, plugins, scaffolded skills, and TUI/CLI interactions treat
   "campaign" and "camp" as the same concept.** "Where is my campaign?" must
   work exactly like "where is my camp?". A skill, prompt, or agent instruction
   that recognizes one must recognize the other.
2. **Docs acknowledge the earlier name at least once.** Every repository's
   primary entry point (README, docs index, or onboarding page) says it once,
   in the form "a camp, previously called a campaign", or equivalent. Once is
   enough. Do not repeat it in every paragraph.
3. **No deprecation warning, nag, or correction is ever shown** for
   campaign-era vocabulary. Not in CLI output, not in TUI hints, not in agent
   replies, not in docs asides. There is nothing to deprecate: the technical
   names are intentionally stable and the user synonym is intentionally
   supported.
4. **Never emit a deprecation warning for a frozen contract.** `--campaign`,
   `campaign_root`, `.campaign/`, and every other name in the frozen list are
   supported spellings, not legacy debt. Warning about them would tell users to
   change something that must not change.

## Frozen technical names

None of the following change in this program. They are the compatibility
boundary. Preserve exact spelling, case, path, key, and value.

Renaming any of them is a separate, versioned migration proposal with its own
approval, not part of a vocabulary change.

### Filesystem paths and markers

- `.campaign/` workspace marker directory
- `.campaign/campaign.yaml` camp metadata file
- every path beneath `.campaign/`, including `intents/`, `quests/`,
  `settings/`, `workitems/`, `skills/`, `templates/`, `cache/`, `leverage/`,
  `manifests/`, `watchers.yaml`, `flows/registry.yaml`, `fest/navigation.yaml`,
  `graph.db`, `integrations/buzz.yaml`, and the legacy `scaffold.yaml` fallback
- `~/.obey/campaign/` and its XDG equivalent `$XDG_CONFIG_HOME/obey/campaign/`
- `~/.obey/campaign/config.json` and `~/.obey/campaign/registry.json`
- the `.camp` attachment and link marker file, its schema version, and its
  location beside a linked project root
- `~/.obey/machines.yaml` and machine registry paths

### Environment variables

- `CAMP_ROOT`
- `CAMP_REGISTRY_PATH`, `CAMP_MACHINES_PATH`, `CAMP_MACHINE_NAME`
- `CAMP_QUEST`, `CAMP_WORKITEM_REF`, `CAMP_HOP_ORIGIN`, `CAMP_REMOTE_CAMP_PATH`
- `CAMP_NO_DEFER`, `CAMP_NO_PEER_FALLBACK`, `CAMP_CACHE_DISABLE`
- `CAMP_SCAFFOLD_WORKSPACE_DIR`
- `OBEY_CAMPAIGN_ID`, `OBEY_AGENT`, `OBEY_SESSION`

### Flags and selector grammar

- the `--campaign` flag and its `-c` shorthand on every command that has it,
  including its no-value interactive default
- the switch and scope selector grammar `org/campaign[@tab]`
- the machine-qualified selector grammar `machine:campaign`

### Machine-readable keys

- JSON keys `campaign`, `campaigns`, `campaign_root`, `campaign_id`,
  `campaign_name`, `campaigns_dir`, `active_campaign_id`, `campaign_ids`,
  `in_campaign`
- camelCase equivalents already published by consumers, including `campaignId`
  and `campaignName`
- YAML keys `campaigns` and `campaigns_dir` in the global registry and
  preferences
- the registry file's top-level `campaigns` map
- the scaffold template variable `campaign_name`
- contract entry types `campaign.metadata` and `campaign.registry`
- the Festival installer manifest scope value `campaign`

### Schema versions

Every published `schema_version` string keeps its current value. A wording
change is never a reason to bump a schema. This includes, and is not limited
to, `workitems/v1alpha12`, `concepts/v1alpha1`, `quest-list/v1alpha1`,
`quest-show/v1alpha1`, `quest-links/v1alpha1`, `status-all/v1alpha1`,
`workflow/v1`, `version/v1alpha1`, and the Camp Timeline browser contract
schema. New machine fields, if ever added, are additive and versioned. They are
out of scope here.

### Historical parser formats

- the commit context tag grammar `[obey-campaign:<campaign-id>...]`, including
  its `FE-`, `PH-`, `SQ-`, `qst_`, `WI-`, and `NT-` segments
- the legacy fallback form `[OBEY-CAMPAIGN-<campaign-id>...]`
- the historical doubled `WI-WI-<hex>` workitem ref form
- every fixture and golden file that exists to prove old-format parsing still
  works

Historical parsing is permanent. Do not rewrite fixtures to new vocabulary.

### Code identifiers

- the `internal/campaign` package and every campaign-shaped Go symbol,
  including `CampaignConfig`, `CampaignScope`, `CampaignRoot`, `CampaignDir`,
  `CampaignConfigFile`, and `AppName = "campaign"`
- Rust and TypeScript campaign models in the Festival app and frontends
- Obey's `campaigns` database table, its `campaign_id` foreign keys, and every
  campaign-named protobuf field, message, and RPC method
- shell function and helper names `cgo`, `csw`, `corg`
- skill bundle directory names that contain "campaign", including
  `campaign-commit`, `campaign-structure`, `campaign-workflows`, and
  `cross-campaign`. These are addresses that agents and existing camps resolve
  by name; their prose and descriptions are presentation and do change.

### Things that must never happen

- Do not rename or move `.campaign/`.
- Do not move `~/.obey/campaign/config.json` or `registry.json`.
- Do not rewrite registry keys, camp IDs, project paths, or link markers.
- Do not repurpose `.camp` from an attachment marker into a workspace directory.
- Do not rename JSON, protobuf, database, environment, manifest, or template
  keys in place.
- Do not stop accepting `--campaign` or the existing selector grammar.
- Do not rewrite historical commit prefixes or compatibility fixtures.
- Do not require Camp, Fest, Festival App, Obey, plugins, and generated skills
  to upgrade together.

## What does change

Human-readable presentation, at its source:

- CLI help text, examples, error messages, and success output
- TUI labels, pickers, table headers, and status lines
- README and conceptual documentation prose
- generated shell help descriptions, regenerated from their source
- new-workspace scaffold prose and generated agent instructions
- app and web UI labels
- public site, quickstart, and example copy
- release notes

Generated artifacts are changed at their source and then regenerated. Never
hand-edit generated output.

## Compatibility copy

When a frozen technical name appears in user-facing text, name it as a
compatibility spelling once, in place, without apology and without a warning.

Approved phrasings:

- "the camp metadata directory, `.campaign/`"
- "camp configuration, stored in the compatibility filename `campaign.yaml`"
- "target camp" as the help description for `--campaign`
- "Camp user configuration in `~/.obey/campaign/`"
- "registered camps" for rendered registry output
- "the camp root override, `CAMP_ROOT`"

Do not write "legacy", "deprecated", "old name", "for backwards compatibility
only", or "will be renamed" about anything in the frozen list. The correct word
is "compatibility", and it is used sparingly.

Every product that ships a compatibility FAQ answers these four questions, in
these terms:

- "Should I rename `.campaign/`?" No. It is the stable metadata directory and
  Camp expects it.
- "Is `.camp` the new metadata directory?" No. `.camp` is an attachment marker
  for linked external directories.
- "Do my scripts using `--campaign` or `campaign_root` still work?" Yes.
  Nothing about flags, selectors, or machine-readable output changed.
- "Why do internal files still say campaign?" They are stable compatibility
  contracts that protect your existing data and integrations.

## Public copy rules

These apply to every example in this document and to all copy written under it.

1. **No em dashes anywhere in public copy.** Use a comma, a colon, a period, or
   a rewritten sentence. This applies to help text, docs, release notes, UI
   strings, and marketing.
2. **Never name a competitor**, directly or by allusion, on any public surface.
3. Say what the thing does before saying what it is called.

## Worked examples

### CLI help text

Wrong:

```text
Manage campaign workspaces and switch between campaigns.
  --campaign string   Target campaign by name or ID
```

Right:

```text
Manage your camps and switch between them.
  --campaign string   Target camp by name or ID
```

The flag spelling stays `--campaign`. Only its description changes.

Wrong, because it warns about a stable contract:

```text
  --campaign string   Target campaign (deprecated, use --camp)
```

Right, for an error message:

```text
not inside a camp
Hint: Run 'camp init' to create a camp, or navigate to an existing one
```

The Go sentinel behind that message keeps its identifier. Rename the string,
not the symbol.

### UI labels

Wrong:

```text
[ Campaigns ]        Active Campaign: obey-campaign
Campaign root: /Users/me/work/obey-campaign
Switch Campaign
```

Right:

```text
[ Camps ]            Active camp: obey-campaign
Camp root: /Users/me/work/obey-campaign
Switch camp
```

The camp's own name is user data. Never rewrite a name a user chose, even when
it contains the word "campaign".

### Technical documentation

Wrong, because it renames a frozen path in prose and invites a destructive
action:

```text
Camp stores camp metadata in `.camp/`. Older camps use `.campaign/` and should
be migrated.
```

Right:

```text
Camp stores camp metadata in `.campaign/` at the camp root. That path is
stable: do not rename it. The separate `.camp` file is an attachment marker
written into linked external directories.
```

Also right, for the once-per-repository acknowledgment:

```text
A camp, previously called a campaign, is one workspace holding a group of
related projects and the festivals you run in them.
```

### Release notes

Wrong:

```text
BREAKING: campaigns have been renamed to camps. Migrate your `.campaign/`
directories and update scripts that pass `--campaign`.
```

Right:

```text
Campaigns are now called camps in the product. Existing `.campaign/`
directories, configuration, commands, scripts, and machine-readable output
continue to work unchanged. Do not rename `.campaign/`.
```

This is a presentation change. Release notes must not describe it as breaking,
must not ask for a migration, and must state the "do not rename" line
explicitly, because guessing at a rename is the most likely way a user damages
their own camp.

## Decision rule

When you are unsure whether to change a string:

1. Does a machine read it? A parser, a schema, a script, a stored file, a wire
   format, a database, a test fixture proving old behavior. If yes, freeze it.
2. Does a human read it and never type it? If yes, it is presentation. Change
   it to camp language.
3. Does a human type it? Keep the exact spelling, and describe it in camp
   language. `--campaign` stays `--campaign`, described as "target camp".
4. Still unsure? Freeze it and note it. A missed presentation string is a
   follow-up commit. A changed contract is a broken install.

## Related references

- [`.campaign/` directory reference](campaign-directory-reference.md) for the
  frozen metadata layout.
- [campaign settings files](campaign-settings-files.md) for field-level
  configuration detail.
- [JSON contracts](json-contracts.md) for the machine-readable surfaces and
  their schema versions.
- [error style guide](error-style-guide.md) for how error text is written.
