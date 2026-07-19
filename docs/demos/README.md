# Canonical TUI recordings

These recordings are produced from the real Camp binary in disposable PTYs.
The committed GIFs are the optimized delivery artifacts; raw GIFs, PTY
transcripts, frame captures, and build details remain in the private VHS
evidence bundles.

| Journey | Tape | Delivery GIF | Manifest |
| --- | --- | --- | --- |
| Fresh configure | [fresh-configure.tape](fresh-configure.tape) | [fresh-configure.gif](fresh-configure.gif) | [fresh-configure.manifest.json](fresh-configure.manifest.json) |
| Machine/status | [machine-tui.tape](machine-tui.tape) | [machine-tui.gif](machine-tui.gif) | [machine-tui.manifest.json](machine-tui.manifest.json) |

The private evidence runs are `camp/fresh-configure/1d5415b8` and
`camp/machine-tui/1d5415b8`. Each manifest records the source revision,
artifact hashes/metadata, validation result, and secret-Gist handoff.

## Reproduce

Use disposable fixtures containing the branch binary and fixture state, then
run the real VHS tapes from the repository root:

```sh
CAMP_VHS_ROOT=/path/to/fresh-configure-fixture just vhs record docs/demos/fresh-configure.tape
CAMP_VHS_ROOT=/path/to/machine-fixture just vhs record docs/demos/machine-tui.tape
```

The tapes set a fake `HOME`, fixture `PATH`, and non-sensitive terminal
identity. They write raw output under `out/`; keep raw recordings and PTY
evidence in the private bundle, and publish only the optimized GIF after the
privacy scan passes.
