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

## Known Issues — Pending Fix

The CW0003 review captured several open defects on the v1alpha6 surface.
These are tracked in the festival findings under
`festivals/active/camp-workitem-linking-and-commits-CW0003/`. Until they
land, operators should be aware of the following.

### Doctor reports false positives on intents and festivals

Finding `CW0003-format-01`: `camp workitem doctor` flags non-workitem
artifacts (intents and festivals) as missing-ref. In real repos this can
generate dozens of false positives (52 observed on the obey-campaign
workspace).

Workaround: when reading doctor output, treat entries outside the workitem
status directories as advisory and ignore them. Apply `--fix` only when the
queue is dominated by genuine workitem entries (see next item).

### Doctor `--fix` aborts the queue on the first per-item error

Finding `CW0003-format-06`: `camp workitem doctor --fix` stops processing on
the first per-item failure rather than continuing and reporting a summary at
the end. A single malformed `.workitem` can prevent backfill of every later
workitem in the same run.

Workaround: when `--fix` aborts, address the reported workitem in isolation
(hand-edit or remove the offending file), then re-run `--fix`. Iterate until
the queue completes.

### Validator does not enforce the `WI-<6 hex>` shape on hand-edited markers

Finding `CW0003-format-02`: `validateMetadata` accepts any non-empty string
for `ref`. Hand-edited markers with malformed values (wrong prefix, wrong
length, non-hex characters, embedded whitespace) load successfully and the
junk reference propagates into commit-message tags and git history.

Workaround: only set `ref` via `camp workitem doctor --fix` or by letting
`camp workitem create` derive it. Treat hand-editing the `ref` field as
unsupported until validator coverage lands.

### Commit-path silently drops the `WI-` segment for legacy workitems

Finding `CW0003-format-13`: when a workitem has no `ref`, the commit-path
emits a tag without the workitem segment rather than refusing or warning.
The behavior is intentional for read-side compatibility but is silent, so
operators who expect every commit to carry a workitem reference will not
notice the gap.

Workaround: run `camp workitem doctor --fix` ahead of any campaign-wide
commit history audit, and confirm via `camp workitem doctor` that the
backfill is complete before relying on tag aggregation.

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
