# Transfer

`camp transfer` copies a file between campaigns, including campaigns on another machine.

```
camp transfer <source> <destination> [--force]
```

Both endpoints use the same grammar, and either one may be remote — but not both.

## The grammar

An endpoint is up to three colon-separated parts:

```
                    notes.md              # a path in the current campaign
           mycampaign:notes.md            # a path in another local campaign
   devbox:mycampaign:notes.md             # a path in a campaign on another machine
```

Camp reads the leading segment as a **machine id first**, and only if that id is in your
`~/.obey/machines.yaml`. On a machine with no fleet configured the machine reading is
unreachable, so every pre-existing form keeps its old meaning exactly.

A machine endpoint requires all three parts. Naming a machine without a campaign is an
error rather than a guess:

```
$ camp transfer devbox:notes.md .
Error: "devbox" is a machine; use machine:campaign:path (for example devbox:<campaign>:notes.md)
```

### Shadowing, and the `local:` escape

If a campaign happens to share a name with a registered machine, the machine wins — and
camp tells you, every time, because the ambiguity is in the command you just typed:

```
$ camp transfer devbox:notes:file.md .
camp: devbox is a registered machine; reading it as machine:campaign:path (use local:devbox:... for the campaign)
```

`local:` forces the campaign reading:

```
$ camp transfer local:devbox:file.md .
```

The note fires on every invocation rather than once per session. A silently different
reading on the second run is precisely what it exists to prevent.

## Copy semantics

Transfer is a **copy**. The source is never modified, moved, or removed.

**The destination is never overwritten without `--force`.** This holds on every path,
including the scp fallback described below:

```
$ camp transfer notes.md devbox:mycampaign:notes.md
Transferred notes.md -> devbox:mycampaign:notes.md

$ camp transfer notes.md devbox:mycampaign:notes.md
Error: destination exists on devbox, not overwritten (use --force)

$ camp transfer notes.md devbox:mycampaign:notes.md --force
Transferred notes.md -> devbox:mycampaign:notes.md
```

Camp reports "Transferred" only when bytes actually moved. A skipped copy is never
reported as a success.

## Transport

Remote transfers prefer **rsync over ssh**, reusing the same ControlMaster socket the rest
of camp uses, so a transfer right after a hop costs no new handshake. rsync gives the
no-clobber guarantee directly via `--ignore-existing`.

If the far machine has no rsync, camp falls back to **scp**. scp has no portable
no-clobber flag, so camp looks before it copies: a stat for a local destination, one
`ssh … test -e` for a remote one. A probe that fails for any reason other than "not there"
is an error, never permission to overwrite.

If neither is available the error names both:

```
Error: rsync not found on devbox and scp not found locally; install rsync on both
machines, or scp on this one
```

Transfers are bounded by a stall timeout rather than a wall-clock deadline. A slow link on
a large file is still making progress; a dead connection is not, and that is the distinction
worth acting on.

## Both endpoints remote

Refused:

```
$ camp transfer devbox:a:f.md buildhost:b:f.md
Error: at most one endpoint may be on another machine (got devbox and buildhost)
```

Brokering the copy would need the two far machines to reach each other, which camp
cannot observe from here and therefore will not promise. Run the transfer from one
of them.

## Version skew

If a previous `camp machine diagnose` saw a different camp version on the far machine,
transfers and hops warn:

```
camp: camp on devbox is v0.9.1, this machine is v0.10.0; features may not match (run 'camp machine diagnose devbox' to re-check)
```

(One line on stderr; wrapped here only if your terminal is narrow.)

Endpoint grammar is parsed **locally**, so a skewed remote does not change how your command
is read. What can differ is the remote's own behavior once camp there takes over — which is
why the warning points at `diagnose` rather than trying to reconcile anything itself.

Update the far machine when you see this. There is no automatic remote update: camp does not
install software on your machines.

## See also

- [machine-mesh.md](./machine-mesh.md) — the mesh model, consent, and the reachability matrix
- `camp machine diagnose <id>` — when a transfer fails on reachability
