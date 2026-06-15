# Camp Exit Codes

Camp uses one CLI-wide exit code table so shell wrappers and agent callers can
handle failures consistently.

| Code | Meaning | Notes |
| --- | --- | --- |
| 0 | Success | The command completed successfully. |
| 1 | Runtime failure | The invocation was valid, but the command found a problem or failed while running. |
| 2 | Usage error | Bad flags, bad arguments, or another invocation-shape problem. |
| 3+ | Command-specific partial state | The command reached a meaningful partial outcome; each command documents the specific meaning. |

## Command-Specific Code 3

| Command | Code | Meaning |
| --- | --- | --- |
| `camp doctor` | 3 | `--fix` repaired some issues but not all detected issues. |
| `camp sync` | 3 | Post-sync validation failed after sync/update work ran. |
| `camp clone` | 3 | The campaign was partially cloned, or post-clone validation failed. |

## Migration Notes

Camp is not yet publicly released, so CH0001 intentionally collapses the old
per-command tables before launch:

- `camp doctor` no longer distinguishes warnings and errors by process code;
  both return 1 because both require attention from automation.
- `camp sync` maps preflight and sync/update failures to 1.
- `camp clone` maps partial success to 3, not 2, so code 2 can consistently
  mean usage error across the CLI.

Commands return typed `CommandError` values internally and `cmd/camp/main.go`
maps those to process exit codes. Command implementations should not call
`os.Exit` from inside Cobra `RunE` handlers.
