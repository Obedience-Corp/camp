# README terminal recordings

These GIFs were recorded with VHS from the real Camp binary at `be046a13`
(v0.11.1 plus 7 commits), using the stable profile on Linux arm64. The sessions
run in a disposable Docker container with a separate home, no network access,
and synthetic projects and work items. Fixture IDs and timestamps are fixed
before recording.

| Asset | Journey | Optimized size |
| --- | --- | --- |
| [cgo-navigation.gif](cgo-navigation.gif) | Fuzzy project navigation, design shortcut, and switching camps | 65,270 bytes |
| [tui-workitems.gif](tui-workitems.gif) | Work dashboard, selection, search, and clearing the search | 205,200 bytes |

Capture uses a 1200 × 760 terminal with the Obedience Corp theme and explicit
truecolor settings (`NO_COLOR` unset, `TERM=xterm-256color`,
`COLORTERM=truecolor`). The published GIFs are optimized to 960 × 608 at 20 fps.

Validation includes an independent PTY run rendered with pyte, assertions on
navigation destinations and filter results, noninteractive work-item JSON
readback, privacy scans, and visual inspection of optimized frames.

Reproducible tapes, fixture setup, and the PTY driver are maintained in the
private experience lab under `vhs/camp/readme-current-cli/`. Generated evidence
stays in its ignored output directory. Only the optimized assets belong here.
