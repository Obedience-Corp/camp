# Triage review — run-20260810T140000Z

> Generated from run `run-20260810T140000Z` by `camp triage review`.
> **Do not edit.** Verdicts are recorded with `camp triage approve`;
> re-rendering replaces this file and any edits in it are lost.

**Run:** `run-20260810T140000Z`  
**Phase:** reviewing  
**Profile:** `default` (full run)  
**Snapshot:** 10 rows in 4 batches, taken 2026-08-10T14:00:00Z

## Decision requested

Review the proposed portfolio below. Approve it as a batch, approve
selected lanes, or amend individual rows:

```
camp triage approve --lane <lane>
camp triage approve <stable-id> --amend <disposition>
camp triage approve <stable-id> --reject --note "why"
```

Approval authorizes the recorded actions and nothing else. No workitem
is deleted: terminal dispositions move a workitem into the dungeon,
where it stays readable. A consolidated parent is not retired until
every successor it declared exists.

**2 rows have no proposal yet.** They are listed at the end and are not covered by
any approval below.

## Recommended priority order

1. **Current** — 1 row. Work happening now. Applying sets the attention stage; nothing is moved or retired.
2. **Next** — 2 rows. Queued behind the current lane. Applying sets the attention stage only.
3. **Active** — 1 row. Live work outside the current focus. Applying sets the attention stage only.
4. **Promote onto the festival rail** — 1 row. Promoted along the workitem rail. Forward-only; approval is required.
5. **Park for later** — 1 row. Kept and visible, deliberately not being worked. Recoverable at any time.
6. **Close as delivered** — 1 row. Terminal. Nothing is deleted: the workitem moves to the dungeon and stays readable.
7. **Consolidate and retire** — 1 row. Each parent is split into focused successors first. A parent is not retired until every declared successor exists.

## Proposed portfolio decisions

### Current

Work happening now. Applying sets the attention stage; nothing is moved or retired.

| Workitem | Disposition | Action | Rationale |
| --- | --- | --- | --- |
| `camp-intent-notes-tui` | current | `attention/current` | Fully planned and already the primary implementation thread. |

### Next

Queued behind the current lane. Applying sets the attention stage only.

| Workitem | Disposition | Action | Rationale |
| --- | --- | --- | --- |
| `obey-machine-session-boundary` | next | `attention/next` | — |
| `obey-observation-boundary` | next | `attention/next` | — |

### Active

Live work outside the current focus. Applying sets the attention stage only.

| Workitem | Disposition | Action | Rationale |
| --- | --- | --- | --- |
| `fest-ritual-creation-lifecycle` | active | `attention/active` | — |

### Promote onto the festival rail

Promoted along the workitem rail. Forward-only; approval is required.

| Workitem | Disposition | Action | Rationale |
| --- | --- | --- | --- |
| `festival-hub-control-plane` | ready | `rail/ready` | — |

### Park for later

Kept and visible, deliberately not being worked. Recoverable at any time.

| Workitem | Disposition | Action | Rationale |
| --- | --- | --- | --- |
| `loop-scheduling-primitives` | parked | `attention/parked` | Reconcile with the existing Obey executor after launch; do not build a parallel supervisor. |

### Close as delivered

Terminal. Nothing is deleted: the workitem moves to the dungeon and stays readable.

| Workitem | Disposition | Action | Rationale |
| --- | --- | --- | --- |
| `camp-artifact-commit-updates` | completed | `dungeon/completed` | Delivered by festival CA0004; only narrow follow-up intents remain. |

### Consolidate and retire

Each parent is split into focused successors first. A parent is not retired until every declared successor exists.

| Workitem | Disposition | Action | Rationale |
| --- | --- | --- | --- |
| `platform-adoption-and-extensibility` | consolidate | `split` | Umbrella: split app compatibility, template strategy, and the ob UX audit into focused owners. |

### Awaiting judgment

No live proposal. These rows are in the run but nothing has been decided for them.

| Workitem | Type | Batch |
| --- | --- | ---: |
| `intent-tidy-the-inbox` | intent | 4 |
| `shared-template-sync` | design | 3 |

### Identity exceptions

1 row is identified only by path: no `.workitem` marker resolved during
preflight, so the path is the identity. A move invalidates it.

- `festival-hub-control-plane`

## Resulting portfolio shape

| Result | Count |
| --- | ---: |
| Current | 1 |
| Next | 2 |
| Active | 1 |
| Promote onto the festival rail | 1 |
| Park for later | 1 |
| Close as delivered | 1 |
| Consolidate and retire | 1 |
| Awaiting judgment | 2 |
| **Total** | **10** |

## How this result was produced

1. Snapshotted 10 rows under profile `default`, partitioned into 4 batches by `type`.
2. Gathered evidence per row at the depth its lane's policy asked for.
3. Recorded one proposal per row, resolved through the row's type
   vocabulary into the action camp will perform.
4. Stopped here, at the approval checkpoint, before any mutation.

### Phase history

| Phase | Recorded at |
| --- | --- |
| created | 2026-08-10T14:00:00Z |
| snapshotted | 2026-08-10T14:00:00Z |
| judging | 2026-08-10T14:00:00Z |
| reviewing | 2026-08-10T14:00:00Z |

### Evidence

| Produced by | Records |
| --- | ---: |
| evidence | 7 |
| synthesis | 1 |
| judged without a gathered record | 1 |

Camp calls no models. Every record above was submitted through
`camp triage evidence set` and validated against the
`triage/v1alpha1` schema before it was stored.

## Approval record

| Workitem | Verdict | Disposition | Decided by | Decided at |
| --- | --- | --- | --- | --- |
| `camp-artifact-commit-updates` | approved | completed | lancekrogers | 2026-08-10T14:00:00Z |
| `camp-intent-notes-tui` | approved | current | lancekrogers | 2026-08-10T14:00:00Z |
| `shared-template-sync` | rejected | parked | lancekrogers | 2026-08-10T14:00:00Z |

