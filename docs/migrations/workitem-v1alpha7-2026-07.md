# Workitem `.workitem` Schema v1alpha7 Upgrade Guide (July 2026)

This note covers the v1alpha7 schema bump delivered alongside `camp gather
design`/`camp gather explore` (directory-based workitem gathering).

## Scope

1. `.workitem` files now use `version: v1alpha7`.
2. Two new optional fields are introduced, both written only on source
   workitems that get gathered.
   - `gathered_into` records the id of the combined workitem a source was
     moved into.
   - `gathered_at` is the RFC3339 UTC timestamp of the gather.

## Why

`camp gather <type>` moves selected directory-based workitems inside a new
gathered package. Sources need durable lineage back to the package they were
folded into, recorded on the source's own marker so `git blame`/`git log -M`
and any downstream tooling can follow the move without depending on commit
message parsing.

## What Changed

### Schema

The accepted set is now `v1alpha4` through `v1alpha7`. New workitems written
by `camp workitem create` and `camp workitem adopt` emit v1alpha7. Existing
workitems are untouched until repaired or gathered.

```yaml
version: v1alpha7
kind: workitem
id: design-auth-flow-2026-07-01
type: design
title: Auth Flow
ref: WI-a1b2c3
```

A gathered source additionally carries:

```yaml
version: v1alpha7
kind: workitem
id: design-auth-flow-2026-07-01
type: design
gathered_into: design-unified-auth-2026-07-15
gathered_at: 2026-07-15T18:04:00Z
title: Auth Flow
ref: WI-a1b2c3
```

### Reader Compatibility

`v1alpha4` through `v1alpha6` `.workitem` files remain readable.

- `LoadMetadata` accepts all four versions through `acceptedWorkitemVersions`
  in `internal/workitem/metadata.go`.
- `gathered_into`/`gathered_at` are absent (empty) on every workitem that has
  never been gathered, including legacy ones.

## Migration Path

No operator action is required. `camp workitem doctor --fix` and `camp
workitem repair` bump a legacy marker's `version` to the current value as
part of their existing repair pass; there is no field to backfill for
`gathered_into`/`gathered_at` since they are only ever set by `camp gather`
itself at the moment a source is moved.

## Reference Links

CLI reference:

- `docs/cli-reference/camp_gather_design.md`
- `docs/cli-reference/camp_gather_explore.md`
- `docs/cli-reference/camp_workitem_create.md`
- `docs/cli-reference/camp_workitem_repair.md`

Source:

- `internal/workitem/metadata.go` (schema definition and accepted version
  set)
- `internal/commands/workitem/gather_exec.go` (writes `gathered_into` and
  `gathered_at` via `internal/workitem.RecordGather`)
