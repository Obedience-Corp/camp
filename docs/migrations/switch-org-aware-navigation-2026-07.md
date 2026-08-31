# Switch Org-Aware Navigation Upgrade Guide (July 2026)

This note covers rollout guidance for the org-aware `camp switch` behavior
introduced in July 2026.

## What Changed

`camp switch` now understands the same org and lifecycle axes already used by
`camp list`:

```bash
camp switch --org obey platform
camp switch obey/platform
camp switch obey/platform@p
```

The command also adds:

- `--org <org>` to filter direct resolution, picker candidates, and completion.
- `--status <active|inactive|reference>` to switch within one lifecycle status.
- `--all` to include inactive and reference camps.
- `--json` for a structured selected-campaign payload.

Fuzzy matching is preserved. Org and lifecycle filters reduce the candidate set
before the existing fuzzy matching behavior runs.

## User-Visible Default

`camp switch <name>` now resolves active camps by default. Inactive and
reference camps remain reachable, but they must be requested explicitly:

```bash
camp switch --all old-reference
camp switch --status reference old-reference
camp switch --org obey --all archive
```

This matches the existing `camp list` default and keeps parked/reference
camps out of high-frequency switch completion and picker flows.

## Compatibility Expectations

Existing unscoped active-camp workflows continue to work:

```bash
camp switch camp
camp switch camp@p
camp switch camp --print
```

For inactive/reference camps, update scripts and shell habits to include
`--all` or explicit `--status`.

## Reference Links

CLI reference:

- `docs/cli-reference/camp_switch.md`
