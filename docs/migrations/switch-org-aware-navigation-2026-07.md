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
- `--all` to include inactive and reference campaigns.
- `--json` for a structured selected-campaign payload.

Fuzzy matching is preserved. Org and lifecycle filters reduce the candidate set
before the existing fuzzy matching behavior runs.

## User-Visible Default

`camp switch <name>` now resolves active campaigns by default. Inactive and
reference campaigns remain reachable, but they must be requested explicitly:

```bash
camp switch --all old-reference
camp switch --status reference old-reference
camp switch --org obey --all archive
```

This matches the existing `camp list` default and keeps parked/reference
campaigns out of high-frequency switch completion and picker flows.

## Compatibility Expectations

Existing unscoped active-campaign workflows continue to work:

```bash
camp switch campaign
camp switch campaign@p
camp switch campaign --print
```

For inactive/reference campaigns, update scripts and shell habits to include
`--all` or explicit `--status`.

## Reference Links

CLI reference:

- `docs/cli-reference/camp_switch.md`
