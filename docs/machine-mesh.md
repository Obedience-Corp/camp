# The Machine Mesh

Camp can move you between machines the same way it moves you between campaigns:
`csw devbox:notes` opens a shell in the `notes` campaign on `devbox`. This document
describes the model that makes that safe, and what each piece degrades to when the
network or the far machine does not cooperate.

The short version: **camp never registers a machine for you, never installs a key, and
never claims reachability it has not observed.** Everything below follows from that.

## The fleet file

Machines live in `~/.obey/machines.yaml`:

```yaml
version: 1
machines:
    - id: devbox
      label: Dev Box
      host: devbox.example.ts.net
      auth_method: tailscale-ssh      # or ssh-agent
      ssh_user: alex
```

`id` is what you type. `host` is what ssh dials. They are separate so a machine can be
renamed without rewriting every command you have in muscle memory.

## The origin payload

When you hop, camp exports one environment variable into the remote login shell:

```
CAMP_HOP_ORIGIN=v1;host=devbox.example.ts.net;user=alex;campaign=notes;id=devbox
```

A single line, percent-encoded values, `v1` prefix. It answers exactly one question on
the far side: *where did this shell come from?* That is what makes `csw -` possible
without either machine keeping state about the other.

Fields:

| Field | Meaning | Absent when |
| --- | --- | --- |
| `host` | the origin's reachable name | never |
| `user` | ssh user on the origin | never |
| `campaign` | campaign the hop started in | the hop did not start inside a campaign |
| `id` | the id derived from the origin's own reachable name | the name is empty, or the field was dropped for size |

`campaign` is genuinely optional, and its absence is meaningful. Hop from your home
directory and `csw -` will refuse, because there is no campaign to return to:

```
$ csw -
Error: camp switch -: origin campaign unknown (the outbound hop did not start inside a
campaign); hop back with 'camp switch <machine>:<campaign>'
```

That is the payload being honest rather than guessing.

## Consent: adopt is always yours

A payload tells the far machine where you came from. It does **not** register you
there, and hop-back does not require registration — an unknown origin still works,
built from the payload.

Registration matters when the hop *fails*. Camp phrases the failure by whether it
has a fleet row to point you at:

```
# origin is registered here
Error: run 'camp machine diagnose devbox' to check reachability: <cause>

# origin is not registered here
Error: the origin is not registered here, probe it with: ssh -o BatchMode=yes alex@devbox.example.ts.net true; 'camp machine adopt' registers it so future hops know the way back: <cause>
```

`camp machine adopt` reads the payload, shows a preview, and asks. It requires a TTY:
the confirm-and-write path refuses non-interactive stdin on purpose, so no script and no
agent can add a machine to your fleet on your behalf. Declining is remembered, and the
hint stops.

Adopting a machine you already have is a no-op that says so:

```
$ camp machine adopt
devbox.example.ts.net is already in your fleet as "devbox"; nothing to adopt
```

## Hopping from the fleet screen

`camp machine` lists the fleet; `enter` on a row picks a campaign on that machine
and hops there. The campaign list comes from the same snapshot completion reads, so
the picker opens without dialing; `r` refreshes it live when the snapshot is stale.

The hop itself still goes through `camp switch <id>:<campaign>`. The screen writes
`ssh-hop:<id>:<campaign>` and the shell wrapper turns that into the hop, which is
the identical path `camp list`'s picker takes. Nothing about the remote resolution
is duplicated: the far machine's own registry decides the path, exactly as it does
for a typed `csw devbox:notes`.

This needs the wrapper. A subprocess cannot replace the shell that launched it, so
without `eval "$(camp shell-init zsh)"` the key reports what is missing instead of
silently doing nothing:

```
hop needs shell integration: run eval "$(camp shell-init <shell>)"
```

`t` remains the connection test — the two are separate gestures, because "can camp
reach this?" is a question worth asking about a machine you are not about to enter.

## The hop-back gesture

`csw -` returns to the origin campaign. There is no history file and no daemon: the
gesture is stateless by construction, which is why it survives a machine reboot on
either end and why it cannot drift.

It is registration-*independent*, not fleet-blind. Camp looks for a `machines.yaml` row
whose host matches the payload's, so your own `ssh_user`, `identity_file`, and
`auth_method` win when you have registered that machine; with no match it builds a
transient machine from the payload alone.

If the origin is unreachable, the failure explains and points at the diagnostic rather
than reporting a bare exit code:

```
$ csw -
Error: run 'camp machine diagnose devbox' to check reachability: could not resolve
"notes" on devbox: ... SSH permission denied (publickey) — check ssh-agent keys
(`ssh-add -l`), identity_file, remote authorized_keys, and ssh_user …
```

(Abbreviated; the live classification continues with Tailscale SSH guidance.)

## Self-reference resolves locally

`csw devbox:notes` *on devbox* does not ssh to itself. Camp derives an id from this
machine's own reachable name — character for character the derivation that fills the
payload's `id` field — and resolves locally when it matches the selector:

```
$ camp switch devbox:no --print
Matched: no -> notes
/home/alex/campaigns/notes
```

A registered machine still wins: if the selector's id is in your `machines.yaml`, camp
honors that mapping and hops, on the principle that an explicit operator entry outranks
detection. Listing your own machine under its real id therefore suppresses local
resolution — `local` is the implicit id for this machine and does not belong in the file.

This matters more than it looks. Without it, a command that is correct everywhere becomes
a hang or a self-ssh on exactly one machine in your fleet, which is the machine you are
most likely to be sitting at.

## Visibility without reachability

The mesh does not require symmetric SSH. A machine that can reach you but that you cannot
reach still shows up usefully, because visibility travels as a **pushed snapshot** rather
than a live query.

When machine A successfully enumerates machine B, it also pushes its own campaign-name
list to B. B can then complete `csw A:<tab>` without ever dialing A:

```
$ camp __complete switch devbox:no
devbox:notes
devbox:notebook
```

Pushed entries carry `source=push` and a 24h TTL. A live-queried entry gets 60s, because
it can be refreshed on demand and a pushed one cannot.

## Reachability matrix

Rows are the transport on the *target* machine. "Forward hop" is you dialing it;
"reverse hop" is it dialing you; "visibility" is whether its campaign names appear in
your completion.

| Target transport | Target platform | Forward hop | Reverse hop | Visibility |
| --- | --- | --- | --- | --- |
| Tailscale SSH | Linux | yes | yes | live query |
| Tailscale SSH | macOS | **no** — see below | yes | pushed snapshot only |
| OpenSSH + key | Linux | yes, once the key is installed | yes | live query |
| OpenSSH + key | macOS | yes, once the key is installed | yes | live query |
| any | offline / asleep | fails, bounded | n/a | last snapshot, until TTL |

**The macOS Tailscale SSH cell is the one that surprises people.** The Tailscale SSH
*server* does not run in sandboxed Tailscale GUI builds, which is what the App Store and
standard macOS app are:

```
$ tailscale set --ssh
The Tailscale SSH server does not run in sandboxed Tailscale GUI builds.
```

So a mac can Tailscale-SSH *out* but cannot accept Tailscale SSH *in*. To reach a mac you
either install standalone `tailscaled`, or add a key to its `~/.ssh/authorized_keys` and
use `auth_method: ssh-agent`. Camp will diagnose this and refuse to fix it: installing a
credential on a machine is your explicit act.

Until you do, the mesh still works in the direction that is available, and the mac's
campaign names still reach the other machine by push.

## Degradation

Every cell above that says "no" degrades to something that explains, not something that
hangs:

- **Unreachable target.** Bounded probe, then the diagnose pointer. `camp machine diagnose`
  reports the socket state, the exact probe command it ran, whether camp is present on the
  far side, and whether *this* machine is accepting connections.
- **camp missing on the far side.** Named explicitly, with the variable that fixes it:
  ```
  remote camp not found on devbox (tried "camp" via sh -lc, i.e. the machine's
  login-shell PATH); if camp lives outside that PATH, set CAMP_REMOTE_CAMP_PATH to its
  exact path on that machine
  ```
  This is common on a fresh fleet member: a stock `go install` puts camp in `~/go/bin`,
  which a non-interactive login shell does not have on its PATH.
- **Version skew.** If a previous `camp machine diagnose` observed a different camp version
  on the target, every hop warns, reading that cached result rather than paying for a
  probe:
  ```
  camp: camp on devbox is v0.9.1, this machine is v0.10.0; features may not match (run 'camp machine diagnose devbox' to re-check)
  ```
  It repeats on each hop by design — the condition is still true, and a warning that
  appeared once would be missed by whoever hits the mismatch later. It stops when the
  cache entry expires (12h) or a fresh `camp machine diagnose` finds the versions agree.

  The warning says only that the versions *differ*. Camp does not order them, because the
  remote is as likely to be ahead as behind.
- **Stale visibility.** A pushed snapshot past its TTL is dropped rather than shown, so
  completion is never confidently wrong.

## What camp will not do

- Install, copy, or generate an SSH key.
- Register a machine without an interactive confirmation.
- Enable a remote login service.
- Claim a machine is reachable on the strength of tailnet membership. Being on the same
  tailnet is not being logged in, and camp says so in its hints.

## See also

- `camp machine --help` — the command surface
- `camp machine diagnose <id>` — the one command to run when a hop fails
- [transfer.md](./transfer.md) — moving files across the mesh
