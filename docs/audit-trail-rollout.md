# Campaign audit trail: rollout for existing campaigns

The campaign event ledger (`.campaign/events/<YYYY-MM>/<writer>.jsonl`) is
additive and optional. New campaigns capture events automatically as you run
state-changing camp commands. Existing campaigns with history predating the
ledger can derive a trail on demand. Nothing here is required, and none of it
touches git history.

`camp audit` and `camp event` are dev-only commands (`//go:build dev`); a
stable camp build does not include them. Use a `dev`-profile build to run
anything below.

## Recommended run order

1. `camp audit backfill` (dry run) - shows how many events would be derived from
   your history (tagged commits across linked repos, intent frontmatter, and
   festival status histories) and how many are already captured.
2. `camp audit backfill --apply` - writes the derived `source: backfill` events
   into the standard shard layout.
3. `camp audit reconcile` then `camp audit reconcile --apply` - fills any state
   file facts (intents, festival transitions) that neither live capture nor
   backfill covered. This matters for work that never produced a commit.
4. `camp audit doctor` - the informational bypass report: per-repo
   tagged/degraded/untagged commit counts. Untagged commits are a normal mode
   (wrapper-opt-out), never a violation; the command exits 0 on findings.

Backfill first, then reconcile, then doctor. Backfill covers the commit-derived
facts; reconcile covers the commit-less remainder; doctor reports what remains
unattributed and is the input to opt-in repair.

## What to expect

- Backfill and reconcile are dry runs by default. They only write with `--apply`.
- `source: backfill` and `source: reconciled` events render identically to live
  events in the timeline and graph; only their provenance differs.
- The doctor's numbers are a baseline. As you keep working through camp/fest
  commands, new activity is captured at the state-change boundary regardless of
  commit habits, so the unattributed population stops growing as a blind spot.

## How re-runs behave

- All three write commands are idempotent. Backfill and reconcile derive stable,
  content-derived ids and skip any fact the ledger already captures (live events
  win; a prior backfill or reconcile is recognized), so a second run reports zero
  new events.
- Commit shas are matched on a short prefix, so a commit captured live (short
  sha) and the same commit derived by backfill (full sha) are recognized as one
  fact and never duplicated.
- Repair (`camp audit repair --sha ... --workitem|--festival ... --why ...`)
  appends an attribution event for an already-landed commit. It also uses a
  content-derived id, so repairing the same commit twice converges.

## Multi-machine note

The writer slug in each shard filename is machine-local, so two machines never
write the same shard and the ledger merges without conflict. Running backfill on
two machines derives the same content-addressed ids, so the streams dedupe on
read.
