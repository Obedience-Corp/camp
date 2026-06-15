# Camp JSON Contracts

This document records the stable JSON surfaces used by agents and scripts.
It resolves the CW0003 review findings `CW0003-uxa-01`, `CW0003-uxa-07`,
and `CW0003-json-01` through `CW0003-json-07`.

## Versioning

Promoted top-level JSON payloads include `schema_version`. CW0003 alpha
surfaces use `<surface>/v1alpha1` unless an older schema is already shipped and
is being preserved for compatibility. Some legacy surfaces in this table only
use the schema string for structured error envelopes while preserving their
existing success payload shape.

Schema versions in this release:

| Command | Schema version | Notes |
| --- | --- | --- |
| `camp workitem --json` | `workitems/v1alpha6` | Workitem dashboard contract with `stage_vocabulary`, explicit `none` stage, ritual/chains festivals, and non-omitempty workflow counters/booleans. |
| `camp workitem create --json` | `workitem-create/v1alpha1` | Create response with next-step hint. |
| `camp workitem link --json` | `workitem-links/v1alpha1` | Emits one `link`. |
| `camp workitem unlink --json` | `workitem-links/v1alpha1` | Emits `removed`. |
| `camp workitem links --json` | `workitem-links/v1alpha1` | Emits `links`. |
| `camp workitem current --json` | `workitem-current/v1alpha1` | Emits local current selection. |
| `camp workitem resolve --json` | `workitem-resolve/v1alpha1` | Emits resolver result and trace. |
| `camp workitem doctor --json` | `workitem-doctor/v1alpha1` | Emits findings; exits 2 when error findings exist. |
| `camp workitem commit --json` | `workitem-commit/v1alpha1` | Emits staging plan and optional commit SHA. |
| `camp workitem commits --json` | `workitem-commits/v1alpha1` | Emits matching commits and per-repo query errors. |
| `camp workitem priority --json` | `workitem-priority/v1alpha1` | Emits `cleared: true` when priority is cleared. |
| `camp concepts --json` | `concepts/v1alpha1` | Emits configured campaign concepts with `generated_at`, `campaign_root`, and concept metadata. |
| `camp intent list/find/show --json` | `intents/v1alpha1` | `--format json` is a deprecated alias. |
| `camp intent count --json` | `intents/v1alpha1` | Counts are emitted as status-count objects in `items[]`; `--format json` is a deprecated alias. |
| `camp intent add --json` | `intents/v1alpha1` | Emits created `id` and `path`. |
| `camp clone --json` | `clone/v1alpha1` | Existing clone result shape; setup and validation failures use the JSON error envelope. |
| `camp doctor --json` | `doctor/v1alpha1` | Existing health result shape on stdout; discovered error findings also emit a JSON error envelope on stderr with the same exit code. |
| `camp leverage --json` | `leverage/v1alpha1` | Existing leverage result shape; refusals use the JSON error envelope. |
| `camp leverage history --json` | `leverage-history/v1alpha1` | Existing history result shape; refusals use the JSON error envelope. |
| `camp quest list --json` | `quest-list/v1alpha1` | Dev-profile quest listing; refusals use the JSON error envelope. |
| `camp quest show --json` | `quest-show/v1alpha1` | Dev-profile quest metadata; refusals use the JSON error envelope. |
| `camp quest links --json` | `quest-links/v1alpha1` | Dev-profile quest links; refusals use the JSON error envelope. |
| `camp skills status --json` | `skills-status/v1alpha1` | Existing skill projection status shape; failures use the JSON error envelope. |
| `camp status all --json` | `status-all/v1alpha1` | Emits `schema_version`, `timestamp`, optional `campaign_root`, and `repos`; an empty campaign emits `repos: []`. |
| `camp sync --json` | `sync/v1alpha1` | Existing sync result shape; returned failures use the JSON error envelope. |
| `camp worktrees info --json` | `worktrees-info/v1alpha1` | Deprecated compatibility surface; failures use the JSON error envelope. |
| `camp worktrees list --json` | `worktrees-list/v1alpha1` | Deprecated compatibility surface; failures use the JSON error envelope. |
| `camp workflow create --json` | `workflow/v1` | Existing workflow collection contract. |
| `camp workflow list --json` | `workflow/v1` | Existing workflow collection contract. |
| `camp workflow show --json` | `workflow/v1` | Existing workflow collection contract. |
| `camp workflow shortcut add --json` | `workflow/v1` | Existing workflow collection contract. |
| `camp workflow doctor --json` | `workflow/v1` | Emits findings; exits 2 when error findings exist. |
| `camp workflow sync --json` | `workflow/v1` | Existing workflow repair-plan contract. |
| `camp version --json` | `version/v1alpha1` | Emits version, build metadata, platform, and build profile. Uses snake_case keys only; legacy camelCase keys were dropped before public release. |

## Scope: contract vs best-effort

Surfaces in this table have a **versioned contract boundary**: promoted success
payloads carry `schema_version`, and legacy success payloads use the listed
schema string for their structured error envelope until their body shape is
promoted.

Rows marked as preserving an existing result shape are formalizing the error
envelope first; their success payload remains the pre-existing JSON object or
array until a future contract bump promotes a `schema_version` field into the
success body.

Surfaces NOT in this table (for example, `camp list --json`,
`camp project list --json`, and `camp __manifest`) are **best-effort**: they
have JSON output but no formal version guarantee until explicitly promoted.

## Path Semantics

All JSON path fields are campaign-relative. To resolve a path to an absolute
filesystem path, use the payload root:

```go
abs_path = filepath.Join(campaign_root, relative_path)
```

`campaign_root` is symlink-resolved with `filepath.EvalSymlinks`; on macOS,
this normalizes paths such as `/tmp` to `/private/tmp`. Consumers should use
the `campaign_root` from the payload instead of resolving roots independently.

The `camp status all --json` campaign-root repo entry uses `.` as its relative
path. Dev-profile quest JSON follows the same path rule while preserving its
existing success payload shape.

## Field Rules

JSON field names are `snake_case`. Registry structs preserve their YAML tags
for files on disk while using explicit JSON tags for command output.

Empty result sets emit arrays, not `null`. For example,
`camp workitem commits <selector> --json` emits `"commits": []` when no
matching commits are found.

## Error Envelope

When a command supports `--json` and refuses the invocation before producing a
domain result, it writes an error envelope to stderr and exits non-zero:

```json
{
  "schema_version": "workitem-commit/v1alpha1",
  "error": {
    "code": "validation_error",
    "message": "no workitem context resolved from cwd",
    "hint": "camp workitem current <selector>",
    "exit_code": 2
  }
}
```

Validation and not-found refusals exit 2. Unexpected runtime failures exit 1
unless the underlying command already carries a more specific exit code.

Doctor commands are the exception for discovered findings: they emit their
normal findings payload on stdout and exit 2 when error-severity findings are
present. Setup failures before findings are produced use the error envelope.

## Agent Manifest

`agent_allowed=true` means the command has a supported non-interactive mode
for agents. JSON support is required for agent-facing structured output, but
JSON support alone does not make every command agent-allowed. Mutating workflow
repair surfaces remain operator-oriented unless they are explicitly annotated
in the command manifest.
