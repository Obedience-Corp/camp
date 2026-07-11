# Workitem Validate and Repair Reference

`camp workitem validate` and `camp workitem repair` keep workflow work item
directories consistent with the current `.workitem` schema. Validate is the
read-only structural checker; repair is the one-command, non-destructive fix.

## Command boundary: doctor vs validate vs repair

The three commands share internals but have distinct purposes:

| Command | Scope | Mutates |
| --- | --- | --- |
| `camp workitem doctor` | The link registry, current-workitem, and priority stores. Also backfills refs across every discovered workitem. | `--fix` only |
| `camp workitem validate` | The structure of workflow work item directories on disk and their `.workitem` markers. | Never |
| `camp workitem repair` | One workflow directory: creates or upgrades its `.workitem` marker to the current schema. | Unless `--dry-run` |

Doctor answers "is the registry healthy?". Validate answers "are these
directories tracked correctly?". Repair answers "make this directory a valid
work item". Validate and repair reuse doctor's finding-code conventions and the
`create` / `adopt` id and ref generation so agents see one consistent contract.

## What counts as a work item directory

Validate mirrors discovery when it decides which directories to scan:

- `workflow/design/<dir>` and `workflow/explore/<dir>` are work items by
  location. Every child directory is a candidate, marker or not, so a legacy
  directory with no marker is reported.
- `workflow/<custom-type>/<dir>` (for example `workflow/feature/<dir>`) is a
  work item only when it already carries a `.workitem` marker. An unmarked
  custom directory is intentionally not a work item and is never flagged.
- `dungeon` directories, hidden directories, `workflow/intent`, and
  `workflow/festival` are control areas and are always skipped.

A work item directory that sits directly under `workflow/<type>/` is legitimate
as is. Neither command asks you to add a status subdirectory and repair never
moves or renames a directory.

## Finding codes

`camp workitem validate --json` emits stable finding codes. Each finding carries
a `severity`, the campaign-relative `target` directory, a `message`, the exact
`repair_command`, and a `repairable` boolean.

| Code | Severity | Meaning | Repair |
| --- | --- | --- | --- |
| `workitem.marker.missing` | error | A design or explore directory (or an explicitly targeted directory) has no `.workitem` marker. | Creates a marker. |
| `workitem.marker.malformed` | error | The `.workitem` marker cannot be read, cannot be parsed as YAML, or violates the schema (bad kind, empty id, unsupported version, malformed ref or quest id). | Repairs when the marker parses as YAML; refuses an unparseable marker (`repairable: false`). |
| `workitem.type.mismatch` | error | The marker `type` field disagrees with the path segment after `workflow/`. The path is authoritative. | Rewrites `type` to the path segment. |
| `workitem.ref.missing` | warning | The marker is valid but has no `ref`. Shared with `camp workitem doctor`. | Backfills a `WI-<6 hex>` ref. |
| `workitem.schema.outdated` | warning | The marker uses an accepted legacy schema version (`v1alpha4` or `v1alpha5`). | Upgrades `version` to the current schema. |

Validate exits `2` when any error-severity finding is present. Warnings alone
exit `0`. Setup failures (for example running outside a campaign) use the JSON
error envelope described in `docs/json-contracts.md`.

## Repair behavior

Repair takes exactly one path of the form `workflow/<type>/<dir>` and refuses
anything else as ambiguous. It is idempotent and non-destructive:

- The directory is never moved or renamed and document contents are never
  touched. Only the `.workitem` marker is written.
- The workflow type is inferred from the path segment after `workflow/`. Use
  `--type` to override it.
- Titles come from the first markdown H1 of the primary doc (README.md or the
  first top-level `.md`), falling back to the humanized directory name. An
  existing non-empty title is preserved.
- The id and ref are generated with the same rules as `create` and `adopt`. An
  existing id is preserved; a missing or malformed ref is regenerated.
- Legacy but accepted schema versions are upgraded to the current schema. A
  marker that cannot be parsed as YAML is refused so nothing is clobbered; fix
  or remove it by hand first.
- A malformed `quest_id` is cleared because an unusable quest link would keep
  the marker invalid.

Repair prints exactly what changed. A directory that is already valid reports no
changes. Use `--dry-run` to preview and `--json` for a machine-readable result
that includes `created_marker`, `changed`, the ordered `changes`, and the
resulting `workitem` identity.

## Examples

```
# Scan the whole campaign and print repair commands for any problems.
camp workitem validate

# Machine-readable findings for an agent harness.
camp workitem validate --json

# Validate a single directory.
camp workitem validate workflow/design/api-refactor

# Fix a legacy directory (creates or upgrades its marker).
camp workitem repair workflow/design/api-refactor

# Preview the fix without writing.
camp workitem repair workflow/design/api-refactor --dry-run
```
