# Peer transport

Camp can pull git objects and artifacts from another machine in your fleet instead of
from origin. On a LAN or a tailnet that is usually much faster, and for a fresh machine
it is the difference between a coffee break and a minute.

It is an optimization and never a dependency. Everything here degrades to the ordinary
origin path, out loud, and no peer feature changes what `camp sync` or `camp clone` do
when you do not ask for one.

Machines come from `~/.obey/machines.yaml`; see [machine-mesh.md](machine-mesh.md) for
setting one up. Every example below uses a machine id from `camp machine list`.

## Seeding a new machine

```
camp clone git@github.com:you/campaign.git ~/campaigns/campaign --from studio-mac
```

Camp asks `studio-mac` for the campaign, copies what it can from that machine, then
points `origin` at the URL you gave and fetches whatever the peer did not have. The
result is an ordinary clone of origin: same commit, same tree, same branch, same
`origin` remote. How the bytes arrived is a transport detail that does not survive into
your repository.

Without `--from`, `camp clone` behaves exactly as it always has.

### How camp decides what to copy

For each repository — the campaign root and every submodule — camp asks the peer three
questions in one ssh round-trip: is the working tree clean, is a git operation in
flight, and is `HEAD` readable. A repository that answers cleanly is **quiescent**, and
camp byte-copies its object store directly. Anything else falls back.

The check is per repository, not per campaign. One dirty submodule does not stop the
other twenty from taking the fast path.

Only immutable content is ever copied: objects and refs. Never an index, never config or
hooks, never a working tree. And camp re-checks `HEAD` **after** the copy — if the peer
moved while camp was reading, the copy is discarded and that repository falls back
rather than arriving half-written.

Anything a git operation could be writing to counts as busy: `index.lock`, an in-flight
merge or rebase, and locks under `refs/` (a fetch holds one of those while writing into
the very pack directory a copy reads).

### When a repository is not quiescent

Camp streams a `git bundle` instead. A bundle is written in one pass by the peer's git
and verified before camp reads a single object out of it, so it is correct even while
someone is working on that machine. It costs more CPU on the peer, which is exactly what
the copy path exists to avoid — so camp only pays it when it must.

If the bundle fails too, camp clones from the peer over ssh; if that fails, it clones
from origin. Each step says why it stepped down.

### What actually happened

```
camp clone git@github.com:you/campaign.git ~/campaigns/campaign --from studio-mac --json
```

```json
{
  "success": true,
  "seed": {
    "repos": [
      { "repo": ".", "method": "pack-copy" },
      { "repo": "projects/api", "method": "bundle",
        "reason": "peer repository is not quiescent: uncommitted changes in the working tree" }
    ]
  }
}
```

`method` is one of `pack-copy`, `bundle`, `peer-clone`, or `origin`. `reason` appears
only when camp used something slower than it wanted, and says why.

A clone with no `--from` emits **no `seed` key at all**, so existing scripts see exactly
the JSON they saw before.

## Syncing an existing campaign

```
camp sync --from studio-mac
```

Git objects are fetched from the peer into a private namespace and verified by git on
arrival, exactly like anything fetched from origin. Your checkout is not moved by the
peer: a peer is a source of objects, not a source of truth.

## Artifacts

Artifacts are the files you do not commit — renders, datasets, media. Declare a
directory and camp will carry it between machines:

```
camp artifacts add "Final Renders"
camp artifacts list
camp sync --from studio-mac
```

### Camp will not overwrite your work

A pull only replaces a local file whose bytes are exactly the last state agreed with
that peer. If you edited it since, or camp has never seen it, the file is **protected**:
left alone and reported.

That protection is sticky on purpose — it survives every later sync, so a conflict
cannot quietly resolve itself the next time you are not looking.

```
$ camp sync --from studio-mac
Artifacts:
  ⚠ Final Renders (synced; 1 conflict kept local)
      hero.psd (local edit preserved)
      Resolve with: camp artifacts resolve <path> --from <machine> --take-local|--take-peer
```

### Resolving a conflict

Look first — this changes nothing:

```
camp artifacts resolve --list --from studio-mac
```

```
Open conflicts with studio-mac:

  Final Renders/hero.psd
      yours: 84213 bytes    last agreed: 61044 bytes (2026-08-04 17:22)

Resolve with:
  camp artifacts resolve <path> --from studio-mac --take-local|--take-peer
```

Then pick a side, one path at a time:

```
camp artifacts resolve "Final Renders/hero.psd" --from studio-mac --take-peer
```

- **`--take-peer`** fetches that one file from the peer, puts it in place, and records it
  as agreed. The conflict is gone and both machines match.
- **`--take-local`** keeps your copy. That path is then **pinned local for that peer**:
  camp will not overwrite it from that machine again, which also means later changes to
  it *on that machine* will not arrive on their own. Run `--take-peer` when you want
  them.

`--take-local` works with the peer offline. Keeping what you already have should not
depend on the machine you are disagreeing with.

There is deliberately no `--all`. Resolving a whole conflict list in one command is
exactly the accident the protection exists to prevent; loop the per-path form if you
really mean it.

JSON is available on both forms:

```
camp artifacts resolve --list --from studio-mac --json
```

```json
{
  "conflicts": [
    {
      "root": "Final Renders",
      "path": "hero.psd",
      "peer": "studio-mac",
      "localSize": 84213,
      "localMtimeUnixNano": 1785935913607889433,
      "agreedSize": 61044,
      "agreedMtimeUnixNano": 1785935909394150742,
      "agreedAt": "2026-08-04T17:22:41.900843806Z"
    }
  ],
  "peer": "studio-mac"
}
```

`agreedAt` is when that baseline was recorded — the conflict began some time after it.
Camp does not keep a separate "first noticed" timestamp.

## rsync: which engine you get, and why it might be slow

Artifact transfer uses rsync, and "rsync" is not one program. A Mac can easily have two
at once — Homebrew's genuine rsync early on `PATH`, and Apple's openrsync at
`/usr/bin/rsync`:

```
$ rsync --version | head -1
rsync  version 3.4.4  protocol version 32

$ /usr/bin/rsync --version | head -2
openrsync: protocol version 29
rsync version 2.6.9 compatible
```

Camp asks the binary it is about to run, on **both** machines, rather than guessing from
the operating system. It uses the delta engine only for genuine rsync at protocol 30 or
newer, on both ends — rsync negotiates down to the lower of the two, so one weak end
decides the transfer. Anything else, including anything camp cannot identify, gets an
honest whole-file copy instead of a delta that cannot be trusted.

Whole-file transfers are resumable at file granularity: an interrupted file continues
rather than restarting.

`camp sync --json` reports which engine ran, per artifact root:

```json
{
  "artifacts": [
    {
      "root": "Final Renders",
      "synced": true,
      "protected": 1,
      "skippedConflicts": ["hero.psd"],
      "engine": "whole-file",
      "engineReason": "local: openrsync (protocol 29); camp does not use its delta engine"
    }
  ]
}
```

On a healthy pair the same field reads `"engine": "rsync-delta"` with no
`engineReason` at all.

If a sync is slower than you expect, that field is the first thing to read. `engine` is
`rsync-delta` or `whole-file`; `engineReason` is present only on the slower path.

### The 24-hour cache, and the surprise it can cause

The engine verdict is cached per machine for **24 hours**, because probing costs an ssh
round-trip and the answer only changes when someone installs or removes an rsync.

**This is worth knowing before it puzzles you:** if you install a faster rsync to speed
up syncing, camp will not notice for up to a day. Nothing looks wrong — syncs simply
keep using the engine that was true this morning. Force a fresh probe:

```
camp sync --from studio-mac --no-probe-cache
```

That re-probes both ends and writes the new verdict back, so it repairs the stale entry
rather than merely bypassing it once. Check it took effect by reading `engine` in
`--json`.

## What camp guarantees

- **Git content is verified by git.** Packs are indexed by git, refs updated by git,
  checkouts performed by git. A peer never substitutes for that.
- **Your local bytes are never silently overwritten.** An artifact is replaced only when
  it exactly matches the last agreed state; `camp artifacts resolve` is the only thing
  that moves that baseline, and it is something you run on purpose.
- **A peer is an optimization.** Unreachable, dirty, or running an ancient rsync, the
  operation still completes by another route, and says which one it used.
- **Defaults do not move.** Without `--from`, output and behaviour are what they were
  before any of this existed — including byte-identical `--json`.

## See also

- [machine-mesh.md](machine-mesh.md) — registering machines and ssh setup
- [transfer.md](transfer.md) — `camp transfer` for one-off file copies
- `camp clone --help`, `camp sync --help`, `camp artifacts resolve --help`
