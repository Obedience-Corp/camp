# Camp Examples

This directory holds a small curated set of examples that are intended to stay aligned with the live Camp CLI.

The examples here are reference material, not a comprehensive tutorial set. If a workflow is better explained by command help or the main docs, prefer those sources over adding another example script.

## Included examples

| File | Purpose |
|------|---------|
| [jumps.yaml](jumps.yaml) | Example `.campaign/settings/jumps.yaml` with navigation and command shortcuts |
| [project-management.sh](project-management.sh) | Project add/list/remove flows and a simple scripting loop |

## Usage

- Read the files directly for copy-pasteable examples.
- Use `camp --help`, `camp project --help`, and `camp shortcuts --help` for the authoritative command contract.

## Shell integration

Navigation examples that use `cgo` require shell integration:

```bash
# zsh
eval "$(camp shell-init zsh)"

# bash
eval "$(camp shell-init bash)"

# fish
camp shell-init fish | source
```
