# Triage

Triage is a recorded session that decides what every workitem in this campaign
should become, and then makes it so. It reads your work, proposes a disposition
for each item, waits for you to approve, applies the approved decisions through
camp's own commands, and proves afterwards that the campaign matches what you
approved. Nothing is deleted and nothing terminal happens without your recorded
approval.

Start one:

```
camp triage start
```

## The phases, and who acts

```
created ──▶ snapshotted ──▶ judging ──▶ reviewing ──▶ applying ──▶ verified
                                 ▲          │             │
                                 └──────────┘             │
                                  refresh stales a row    │
                                                          ▼
                                                     abandoned
```

| Phase | What happens | Who acts |
| --- | --- | --- |
| `created` | The run exists | camp |
| `snapshotted` | The inventory is frozen into a manifest | camp |
| `judging` | Evidence is gathered, dispositions proposed | agents (or you) |
| `reviewing` | Verdicts approved, amended, or rejected | **you** |
| `applying` | Approved verdicts executed, receipts written | camp |
| `verified` | Every applied row checked against reality | camp |

`abandoned` is reachable from anywhere and keeps the state. A refresh that
finds a row's evidence has moved sends it back from `reviewing` to `judging` —
that is the loop in the diagram, and it is normal.

## Where everything lives

```
.campaign/triage/
├── OBEY.md            this guide
├── profile.yaml       how triage behaves here — every key commented
├── types/             per-type policy: what each type may be decided into
├── latest             pointer to the most recent run id
└── runs/<run-id>/
    ├── manifest.json      the frozen inventory + the profile it used
    ├── run.json           the phase and its history
    ├── evidence/          one record per row: what was read
    ├── rationales/        one per proposal: why
    ├── decisions.jsonl    append-only verdict stream — the source of truth
    ├── TRIAGE_REVIEW.md   the document you read
    ├── PRIORITIES.md      what to work on, derived from the verdicts
    ├── apply-plan.json    the compiled commands
    ├── receipts.jsonl     what actually ran, with undo commands
    └── verification.json  post-apply proof (+ VERIFICATION.md)
```

The run directory is the record. `decisions.jsonl` is append-only: verdicts are
events, and the documents are rendered from them. Editing a rendered document
does nothing — re-rendering replaces it.

## Reading the review and recording verdicts

`camp triage review` renders `TRIAGE_REVIEW.md` and, on a terminal, opens the
lane-first flow: lanes, then rows, with terminal actions confirming one at a
time. Off a terminal it just renders, so scripts get the documents and nothing
that waits for input.

Record verdicts however suits you:

```
camp triage approve --batch 1                 # everything in a batch
camp triage approve --lane park-for-later     # one lane
camp triage approve <stable-id>               # one row
camp triage approve <stable-id> --amend parked --note "why"
camp triage approve <stable-id> --reject --note "why"
```

A bulk selector never retires or splits a workitem. Terminal rows are skipped
and named, so approving one is always a deliberate act.

Then:

```
camp triage refresh    # re-check the world before acting on it
camp triage apply      # execute approved verdicts (implicit refresh first)
camp triage verify     # prove the campaign matches what you approved
```

`apply --dry-run` prints the whole plan, including rows it cannot run yet, and
changes nothing.

## What the profile controls

Every key in `profile.yaml` ships with its default and a comment. By phase:

| Key | Phase it affects |
| --- | --- |
| `scope.*` | snapshot — which workitems the run considers |
| `runs.mode` | snapshot — incremental carries unchanged verdicts forward |
| `runs.stale_after_days` | the reminder that a new triage is due |
| `review.group_by`, `batch_size` | review — how rows are batched |
| `review.approval` | review — what one approval covers by default |
| `review.require_rationale` | judging — a proposal without reasoning is refused |
| `evidence.depth_by_stage` | judging — how hard to look, per attention lane |
| `anchors.recheck_minutes` | refresh — how long a cached PR verdict answers |
| `routing.*` | advisory only; camp never calls a model |
| `apply.attention_changes` | apply — non-terminal changes run, or print |
| `outputs.*` | where a rendered copy of the priorities brief is kept |

`types/*.yaml` decides what a given type may be decided *into*. The labels are
yours; the canonical actions they map to are camp's. You can rename `completed`
to `shipped` without triage learning a new mutation.

### What no profile can change

These are product behavior, not configuration. A profile can make them
*stricter*, never looser:

- stable workitem identity and an explicit inventory snapshot;
- recorded provenance for recommendations, approvals, and applied actions;
- recoverable moves with link and registry repair;
- authority restrictions for destructive or terminal decisions;
- advisory-only evidence workers regardless of provider or model;
- delegation to canonical workitem/dungeon mutation services;
- interruption-safe session state;
- stale-inventory detection before application; and
- post-application verification with unexplained mismatches reported.

In practice: terminal moves, splits, and festival promotions always require
your recorded approval, and camp never calls a model.

## The incremental model

The default run is incremental, so the second triage is small. A row's verdict
carries forward from the previous run unless something it depended on moved.

A row comes back for judgment when:

- its identity or any evidence anchor changed — a file's hash, a workitem's
  stage, a festival's status, a PR merging;
- the profile changed in a key that touches *that row* — its type policy, its
  lane's evidence depth, or the vocabulary its verdict was expressed in.
  Cosmetic changes (batch size, export path, routing tiers) never invalidate;
- its disposition no longer maps to an action under the current type policy.

`camp triage status` says why any row lost its carry. `camp triage start --full`
re-reviews everything regardless.

## Recipes

**First-ever triage.** `camp triage start` then `camp triage queue --json` to
see what needs judgment. Record evidence and proposals, then
`camp triage review` and approve. Expect this one to be long; it is the only
one that is.

**Weekly incremental.** `camp triage start` — unchanged rows carry, and you
review only what moved. `camp triage status` shows how much carried.

**Sweep-only pass.** `camp triage start --profile sweep` for a fast metadata
pass with no deep reads, when you want the inbox triaged and nothing else.

**Reviewing someone's proposed batch.** `camp triage review --render-only`,
read `TRIAGE_REVIEW.md`, then approve by lane or amend individual rows. Every
proposal carries its rationale and the evidence it rested on.

**Undoing an applied row.** `receipts.jsonl` records the exact undo command for
every action, derived from where the workitem actually landed. Run it. For a
split, `camp workitem split <parent> --undo` removes the lineage and deletes
only successors nobody has touched.

**When a verdict went stale.** `camp triage refresh` reports what changed and
why. Re-judge those rows and approve again; the rest are untouched.
