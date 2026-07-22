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

The tapes set a fake `HOME`, fixture `PATH`, and non-sensitive terminal
identity. They write raw output under `out/`; keep raw recordings and PTY
evidence in the private bundle, and publish only the optimized GIF after the
privacy scan passes.
