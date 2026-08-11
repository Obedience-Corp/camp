# Canonical TUI recordings

These recordings are produced from the real Camp binary in disposable PTYs.
The committed GIFs are the optimized delivery artifacts; raw GIFs, PTY
transcripts, frame captures, and build details remain in the private VHS
evidence bundles.

| Journey | Tape | Delivery GIF | Manifest |
| --- | --- | --- | --- |
| Fresh configure | [fresh-configure.tape](fresh-configure.tape) | [fresh-configure.gif](fresh-configure.gif) | [fresh-configure.manifest.json](fresh-configure.manifest.json) |
| Machine/status | [machine-tui.tape](machine-tui.tape) | [machine-tui.gif](machine-tui.gif) | [machine-tui.manifest.json](machine-tui.manifest.json) |
| Machine dual-auth CLI | [machine-dual-auth.tape](machine-dual-auth.tape) | [machine-dual-auth.gif](machine-dual-auth.gif) | (WI-ca06e1 record-time proof; private gist optional) |
| Project-aware worktree list | [worktree-list.tape](worktree-list.tape) | [worktree-list.gif](worktree-list.gif) | — |
| Tailscale SSH approval | [machine-tailscale-check.tape](machine-tailscale-check.tape) | private gist | (PR evidence bundle) |
| Diagnose an unresolvable host | [machine-diagnose-dns.tape](machine-diagnose-dns.tape) | private gist | (PR evidence bundle) |
| Hop survives a MagicDNS outage | [machine-hop-dns-fallback.tape](machine-hop-dns-fallback.tape) | private gist | (PR evidence bundle) |

The private evidence runs are `camp/fresh-configure/1d5415b8` and
`camp/machine-tui/1d5415b8`. Each manifest records the source revision,
artifact hashes/metadata, validation result, and secret-Gist handoff.

## Reproduce

Use disposable fixtures containing the branch binary and fixture state, then
run the real VHS tapes from the repository root:

```sh
CAMP_VHS_ROOT=/path/to/fresh-configure-fixture just vhs record docs/demos/fresh-configure.tape

# Machine TUI must use full truecolor (fire palette). Agent shells often set
# NO_COLOR — use record-color or an equivalent env strip:
CAMP_VHS_ROOT=/path/to/machine-fixture just vhs record-color docs/demos/machine-tui.tape
# If the just module cwd loses the tape path, run from repo root:
env -u NO_COLOR -u CLICOLOR TERM=xterm-256color COLORTERM=truecolor \
  FORCE_COLOR=1 CLICOLOR_FORCE=1 CAMP_VHS_ROOT=/path/to/machine-fixture \
  vhs "$(pwd)/docs/demos/machine-tui.tape"

CAMP_VHS_ROOT=/path/to/machine-fixture \
  env -u NO_COLOR TERM=xterm-256color COLORTERM=truecolor FORCE_COLOR=1 \
  vhs "$(pwd)/docs/demos/machine-dual-auth.tape"
```

Machine fixture layout (disposable, no live tailnet):

```sh
FIXTURE=$(mktemp -d)
mkdir -p "$FIXTURE/bin" "$FIXTURE/home"
cp bin/camp "$FIXTURE/bin/camp"
cp docs/demos/fixtures/tailscale "$FIXTURE/bin/tailscale"
chmod +x "$FIXTURE/bin"/*
: > "$FIXTURE/machines.yaml"
export CAMP_VHS_ROOT="$FIXTURE"
```

`machine-dual-auth` proves discover defaults to `ssh-agent` and
`--auth tailscale-ssh --user` is honored against the fixture tailnet stub.

`machine-tailscale-check` shows the approval link a check-mode machine needs,
whole and on its own line, in both the detail pane and the hop overlay, and the
`o`/`c` keys handing it to the platform. Its fixture adds a stub `ssh` that
answers the way Tailscale SSH does in check mode, plus stub opener/clipboard
tools that record what they receive, so no live tailnet is contacted and no real
approval URL is published. Build the fixture and assert the journey with:

```sh
just tui pty-machine-tailscale-check          # asserts; builds its own fixture
CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/machine-tailscale-check.tape
```

The pty check is the one that can claim the link is on a single line and that
the exact URL reached the opener and clipboard; the recording is what a reviewer
looks at.

`machine-diagnose-dns` shows `camp machine diagnose` telling "the name never
resolved" apart from "the remote camp is missing or too old" — the two failures
it used to report with the same sentence. It records two machines on purpose:
one whose host resolves and whose camp is still unreachable (which keeps the
generic wording, because for that machine it is accurate), and one MagicDNS name
that does not resolve. The unresolvable name is under an invented tailnet, so
the lookup fails through camp's real resolver rather than a stub; only
`tailscale` and `ssh` are fixture stubs:

```sh
FIXTURE=$(mktemp -d)
mkdir -p "$FIXTURE/bin" "$FIXTURE/home"
cp bin/camp "$FIXTURE/bin/camp"
cp docs/demos/fixtures/tailscale-dns-broken "$FIXTURE/bin/tailscale"
cp docs/demos/fixtures/ssh-unreachable "$FIXTURE/bin/ssh"
chmod +x "$FIXTURE/bin"/*
: > "$FIXTURE/machines.yaml"
CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/machine-diagnose-dns.tape
```

`machine-hop-dns-fallback` shows the same outage with the dial fallback doing
its job: ssh-by-name fails, but diagnose leads with the peer-table address a
hop will actually dial, the `--shell-connect` line dials that address with the
host key pinned to the configured name, and `CAMP_NO_PEER_FALLBACK=1` restores
the plain failure. Its stub `ssh` (`fixtures/ssh-peer-fallback`) answers ONLY
at the peer address, so every working camp command in the recording is proof
the fallback engaged; `fixtures/tailscale-dns-broken-peer` is the DNS-broken
tailnet whose peer table still holds the machine:

```sh
FIXTURE=$(mktemp -d)
mkdir -p "$FIXTURE/bin" "$FIXTURE/home"
cp bin/camp "$FIXTURE/bin/camp"
cp docs/demos/fixtures/tailscale-dns-broken-peer "$FIXTURE/bin/tailscale"
cp docs/demos/fixtures/ssh-peer-fallback "$FIXTURE/bin/ssh"
chmod +x "$FIXTURE/bin"/*
: > "$FIXTURE/machines.yaml"
CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/machine-hop-dns-fallback.tape
```

Prefer a short, neutral fixture root: `--json` prints the ControlMaster socket
path unabbreviated (and the hop's eval line prints the ControlPath), so a
fixture under a long personal path writes that path into the recording and the
privacy scan rejects it.

The tapes set a fake `HOME`, fixture `PATH`, and non-sensitive terminal
identity. They write raw output under `out/`; keep raw recordings and PTY
evidence in the private bundle, and publish only the optimized GIF after the
privacy scan passes.
