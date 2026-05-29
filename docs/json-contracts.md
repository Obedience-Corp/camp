# Camp JSON Contracts

This document records the stable JSON surfaces used by agents and scripts.
It resolves the CW0003 review findings `CW0003-uxa-01`, `CW0003-uxa-07`,
and `CW0003-json-01` through `CW0003-json-07`.

## Versioning

Every top-level JSON payload includes `schema_version`. CW0003 alpha surfaces
use `<surface>/v1alpha1` unless an older schema is already shipped and is being
preserved for compatibility.

Schema versions in this release:

| Command | Schema version | Notes |
| --- | --- | --- |
| `camp workitem --json` | `workitems/v1alpha5` | Existing workitem dashboard contract. |
| `camp workitem create --json` | `workitem-create/v1alpha1` | Create response with next-step hint. |
| `camp workitem link --json` | `workitem-links/v1alpha1` | Emits one `link`. |
| `camp workitem unlink --json` | `workitem-links/v1alpha1` | Emits `removed`. |
| `camp workitem links --json` | `workitem-links/v1alpha1` | Emits `links`. |
| `camp workitem current --json` | `workitem-current/v1alpha1` | Emits local current selection. |
| `camp workitem resolve --json` | `workitem-resolve/v1alpha1` | Emits resolver result and trace. |
| `camp workitem doctor --json` | `workitem-doctor/v1alpha1` | Emits findings; exits 2 when error findings exist. |
| `camp workitem commit --json` | `workitem-commit/v1alpha1` | Emits staging plan and optional commit SHA. |
| `camp workitem commits --json` | `workitem-commits/v1alpha1` | Emits matching commits and per-repo query errors. |
| `camp workflow create --json` | `workflow/v1` | Existing workflow collection contract. |
| `camp workflow list --json` | `workflow/v1` | Existing workflow collection contract. |
| `camp workflow show --json` | `workflow/v1` | Existing workflow collection contract. |
| `camp workflow shortcut add --json` | `workflow/v1` | Existing workflow collection contract. |
| `camp workflow doctor --json` | `workflow/v1` | Emits findings; exits 2 when error findings exist. |
| `camp workflow sync --json` | `workflow/v1` | Existing workflow repair-plan contract. |

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
