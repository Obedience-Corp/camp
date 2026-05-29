# Workitem `.workitem` Schema v1alpha6 Upgrade Guide (May 2026)

This note covers the v1alpha6 schema bump delivered by the CW0003 festival
(workitem linking and commit tracking) and the operator-visible rollout
behavior.

## Scope

The rollout covers a single metadata schema change:

1. `.workitem` files now use `version: v1alpha6`.
2. Two new fields are introduced.
   - `ref` is mandatory on newly created workitems and follows the format
     `WI-<6 lowercase hex>`. It is the deterministic short reference embedded
     in commit messages.
   - `quest_id` is optional. It captures the quest active when the workitem
     was created or adopted (via `--quest` or the `CAMP_QUEST` env var).

## Why

The CW0003 design promotes scoped workitem commits and a campaign-local link
registry. Commit-message tags must carry a compact, stable workitem reference
that is unique within a campaign, and adopted workitems should record the
quest context that produced them.

Design reference: `workflow/design/workitem-linking-commit-tracking/` in
`obey-campaign` (notably `02-workitem-linking-model.md` and
`03-commit-tracking-and-scoped-commits.md`).

## What Changed

### Schema

The accepted set is now `v1alpha4`, `v1alpha5`, and `v1alpha6`. New workitems
written by `camp workitem create` and `camp workitem adopt` emit v1alpha6.

```yaml
version: v1alpha6
kind: workitem
id: feature-CW0003-payments
type: feature
title: Payments rebuild
created_by: lance
ref: WI-a1b2c3
quest_id: q-2026-05-launch
```

Sample diff from v1alpha5 to v1alpha6 for an existing file:

```diff
-version: v1alpha5
+version: v1alpha6
 kind: workitem
 id: feature-CW0003-payments
 type: feature
 title: Payments rebuild
 created_by: lance
+ref: WI-a1b2c3
+quest_id: q-2026-05-launch
```

### Reader Compatibility

`v1alpha4` and `v1alpha5` `.workitem` files remain readable.

- `LoadMetadata` accepts all three versions through `acceptedWorkitemVersions`
  in `internal/workitem/metadata.go`.
- Legacy workitems load with empty `ref` and empty `quest_id`.

### Commit-Path Behavior For Legacy Workitems

When `camp workitem commit` (or `camp p commit` running inside a workitem
context) operates on a legacy workitem that has no `ref`, the commit-message
tag is still emitted, but the `WI-` segment is omitted from the trailer. The
resulting tag therefore identifies the festival or context but not the
specific workitem.

Operator impact: legacy workitems should be backfilled via doctor before
relying on the commit history for per-workitem aggregation. See known issues
below.

## Migration Path

The supported workflow for upgrading an existing campaign:

1. Run `camp workitem doctor` to enumerate workitems with missing `ref` (and
   any other metadata drift).
2. Run `camp workitem doctor --fix` to backfill `ref` values into the
   matching `.workitem` files. Backfilled refs are deterministic for the
   given workitem id and unique within the campaign.
3. Re-run `camp workitem doctor` to confirm no missing-ref workitems remain.

After backfill, subsequent commits emit fully-qualified tags including the
`WI-<ref>` segment.

## Migration Checks and Repairs

The v1alpha6 migration has CLI support for the review cases that used to
require manual operator work:

- `camp workitem doctor` reports missing refs only for directory-backed
  workitems with `.workitem` markers. Intent files and festival directories
  are not treated as ref-backfill targets.
- `camp workitem doctor --fix` continues through per-item ref-backfill
  failures, reports warnings for failed items, and backfills every item it can
  repair in the same run.
- Metadata loading validates `ref` as `WI-<6 lowercase hex>` and validates
  `quest_id` against the supported quest-id shape, so malformed hand-edited
  markers fail fast instead of reaching commit tags.
- `camp workitem commit` attempts to backfill a missing ref for
  directory-backed legacy workitems before composing the commit tag. If that
  backfill fails, the command warns and proceeds without the `WI-` segment.

For campaign-wide history audits, run `camp workitem doctor --fix` first and
then run `camp workitem doctor` again. Treat any remaining missing-ref
findings as items to repair before relying on tag aggregation.

## Compatibility Expectations

- All three accepted schema versions (`v1alpha4`, `v1alpha5`, `v1alpha6`)
  load successfully.
- Writes default to v1alpha6 in `camp workitem create` and `camp workitem
  adopt`.
- Legacy workitems remain usable; their commit tags omit the workitem
  segment until backfilled.
- `camp workitem commit` and `camp p commit` continue to function against
  legacy workitems with the documented silent-omission caveat.

## Reference Links

CLI reference:

- `docs/cli-reference/camp_workitem_create.md`
- `docs/cli-reference/camp_workitem_adopt.md`
- `docs/cli-reference/camp_workitem_doctor.md`
- `docs/cli-reference/camp_workitem_commit.md`

Narrative reference (CW0003):

- `docs/workitem-link-reference.md`
- `docs/workitem-commit-reference.md`
- `docs/workflow-reference.md`

Source:

- `internal/workitem/metadata.go` (schema definition and accepted version
  set)
- `internal/commands/workitem/doctor_ref.go` (backfill path used by
  `doctor --fix`)
