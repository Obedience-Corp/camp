# Command Conventions

## Command Registration: Use Tree B Constructor Style

New cobra commands must use the constructor function pattern (Tree B):

- Command defined as a function returning `*cobra.Command`, for example `NewFooCommand() *cobra.Command`
- Wired at `cmd/camp/root.go` or via a `Register(root *cobra.Command)` function
- No package-level `var cmdFoo *cobra.Command`; no `init()` registration
- Error handling via `camperrors`; JSON output via `jsoncontract`
- Exit codes via `camperrors.CommandError`

Examples of the pattern: `internal/commands/workitem`, `internal/commands/workflow`.

Tree A (package-level vars plus `init()`) exists in older code under `cmd/camp/` subpackages. It is being migrated incrementally; do not add new Tree A commands.

## flow vs workflow naming

`internal/workflow` is the status-directory state machine (`.workflow.yaml`, transitions).
`internal/flow` is a shell-command registry/runner (`.campaign/flows/registry.yaml`).
The CLI command `camp flow` is mostly backed by `internal/workflow`; `camp workflow` manages workflow collections.
These names are inverted relative to what a reader expects. Until they are renamed, both packages carry a doc comment cross-referencing the other.

`camp flow` is hidden from `camp --help`; it is the low-level engine, and `camp promote` is the user-facing front door.
