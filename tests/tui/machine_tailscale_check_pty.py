#!/usr/bin/env python3
"""pty evidence for the Tailscale-approval journey on the machine screen.

The VHS tape shows a reviewer what the screen looks like. This asserts what a
recording cannot: that the approval URL is intact on ONE line (a link split
across a wrap is one nobody can copy), that it survives the hop overlay's own
clamp, and that o/c hand the exact URL to the platform opener and the local
terminal clipboard, including when camp is running across SSH.

It drives the same fixture the tape does — stub ssh and opener, with a fake
remote-session environment — so nothing real is contacted and no live approval
URL is produced.

Writes the evidence bundle files validate-evidence.py consumes:

    <evidence-dir>/pty-transcript.txt
    <evidence-dir>/pty-metadata.json
    <evidence-dir>/screen-snapshots.json

Usage: machine_tailscale_check_pty.py <fixture-dir> <evidence-dir>
"""
import base64
import fcntl
import importlib.metadata
import json
import os
import pty
import select
import struct
import sys
import termios
import time

import pyte

ROWS, COLS = 30, 110

# The URL the fixture ssh stub prints. Invented; see
# docs/demos/fixtures/ssh-tailscale-check.
CHECK_URL = "https://login.tailscale.com/a/f1e2d3c4b5a6"

# camp can stall around five seconds at bubbletea init on a mute pty (a known
# tty-query issue, fixed in fest and still present here).
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 2.5
# The stub holds the connection open the way check mode does, so the test has
# to outlast camp's own 10s hop deadline.
PROBE_BUDGET = 16.0


class Session:
    """One run of camp machine on a pty against the recording fixture."""

    def __init__(self, fixture, args):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.transcript = []
        self.raw = bytearray()
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(fixture)
            binary = os.path.join(fixture, "bin", "camp")
            os.execve(binary, [binary] + args, {
                "HOME": os.path.join(fixture, "home"),
                "PATH": os.path.join(fixture, "bin") + ":" + os.environ.get("PATH", "/usr/bin:/bin"),
                "TERM": "xterm-256color",
                "LINES": str(ROWS),
                "COLUMNS": str(COLS),
                "NO_COLOR": "1",
                "CAMP_MACHINES_PATH": os.path.join(fixture, "machines.yaml"),
                "CAMP_VHS_HANDOFF_LOG": os.path.join(fixture, "handoff.log"),
                # Force the production remote-session path. A remote pbcopy or
                # xclip targets the wrong computer; OSC 52 crosses the PTY to
                # the operator's terminal instead.
                "SSH_CONNECTION": "192.0.2.10 50000 192.0.2.20 22",
                "SSH_TTY": "/dev/pts/camp-fixture",
            })
        # Both the ioctl and LINES/COLUMNS: a small default clips content and
        # sends you chasing a layout bug that does not exist.
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.drain(LAUNCH_BUDGET)

    def drain(self, budget):
        deadline = time.time() + budget
        while time.time() < deadline:
            ready, _, _ = select.select([self.fd], [], [], 0.25)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError:
                return
            if not data:
                return
            self.raw.extend(data)
            self.stream.feed(data)

    def press(self, keys, budget=KEY_BUDGET):
        os.write(self.fd, keys)
        self.drain(budget)

    def snapshot(self, name):
        display = list(self.screen.display)
        self.snapshots.append({
            "name": name,
            "display": display,
            "cursor": {"x": self.screen.cursor.x, "y": self.screen.cursor.y},
        })
        self.transcript.append("===== %s =====" % name)
        self.transcript.extend(line.rstrip() for line in display)
        return display

    def close(self):
        # Every snapshot is already taken, so tear down rather than quitting
        # politely. A graceful Ctrl+C means writing to a pty nobody is reading
        # and then blocking in waitpid on a camp whose own ssh child is still
        # sleeping out check mode — both of which hang this script rather than
        # ending it.
        try:
            os.close(self.fd)
        except OSError:
            pass
        try:
            os.kill(self.pid, 9)
        except OSError:
            return
        deadline = time.time() + 2.0
        while time.time() < deadline:
            try:
                reaped, _ = os.waitpid(self.pid, os.WNOHANG)
            except OSError:
                return
            if reaped:
                return
            time.sleep(0.05)


def whole_line_with(display, needle):
    return any(needle in line for line in display)


def expected_opener():
    """The opener name camp calls on THIS platform.

    Mirrors ui.browserOpenCommand: darwin gets open, everything else xdg-open.
    The fixture installs both names, so asserting the host-specific one remains
    an exact check.
    """
    if sys.platform == "darwin":
        return "open"
    return "xdg-open"


def osc52_clipboard_values(raw):
    """Decode plain OSC 52 system-clipboard writes from a PTY byte stream."""
    marker = b"\x1b]52;c;"
    values = []
    start = 0
    while True:
        begin = raw.find(marker, start)
        if begin < 0:
            return values
        begin += len(marker)
        end = raw.find(b"\x07", begin)
        if end < 0:
            return values
        values.append(base64.b64decode(raw[begin:end]).decode("utf-8"))
        start = end + 1


def main():
    fixture, evidence = sys.argv[1], sys.argv[2]
    os.makedirs(evidence, exist_ok=True)
    handoff = os.path.join(fixture, "handoff.log")
    open(handoff, "w").close()

    failures = []
    snapshots = []
    transcript = []

    # ---- the detail pane, reached with t ----
    s = Session(fixture, ["machine"])
    s.snapshot("fleet")
    s.press(b"\x1b[B")
    s.press(b"t", PROBE_BUDGET)
    detail = s.snapshot("check-detail")

    if not whole_line_with(detail, "Needs Tailscale SSH check"):
        failures.append("detail pane does not name the check-mode failure")
    if not whole_line_with(detail, CHECK_URL):
        failures.append("detail pane does not show the approval URL intact on one line")

    s.press(b"o")
    opened = s.snapshot("link-opened")
    if not whole_line_with(opened, "opened the approval link"):
        failures.append("o did not report handing the link to a browser")

    s.press(b"c")
    copied = s.snapshot("link-copied")
    if not whole_line_with(copied, "copied the approval link"):
        failures.append("c did not report copying the link")
    clipboard_values = osc52_clipboard_values(bytes(s.raw))
    if clipboard_values != [CHECK_URL]:
        failures.append("OSC 52 did not copy the exact URL: %r" % clipboard_values)

    snapshots.extend(s.snapshots)
    transcript.extend(s.transcript)
    s.close()

    # ---- the hop overlay, reached with enter ----
    # --path-output is what the shell wrapper passes; without it camp refuses
    # the hop before the overlay ever opens.
    s = Session(fixture, ["machine", "--path-output", os.path.join(fixture, "hop.txt")])
    s.press(b"\x1b[B")
    s.press(b"\r", PROBE_BUDGET)
    hop = s.snapshot("hop-overlay")

    if not whole_line_with(hop, CHECK_URL):
        failures.append("hop overlay truncated the approval URL")
    if not whole_line_with(hop, "o open it"):
        failures.append("hop overlay does not offer to open the link")

    snapshots.extend(s.snapshots)
    transcript.extend(s.transcript)
    s.close()

    # ---- the writes, which no recording can assert ----
    with open(handoff) as fh:
        handed = [line.strip() for line in fh if line.strip()]
    # Transcript only, never the snapshots array: these lines were read from a
    # file, and screen-snapshots.json is a record of what pyte rendered. The
    # assertion below is the real check either way.
    transcript.append("===== handoff.log =====")
    transcript.extend(handed)

    opener = expected_opener()
    if ("%s %s" % (opener, CHECK_URL)) not in handed:
        failures.append("%s was not handed the exact URL: %r" % (opener, handed))
    if any(line.startswith(("pbcopy ", "xclip ")) for line in handed):
        failures.append("remote copy called a host clipboard tool: %r" % handed)

    # The shapes here are the evidence-bundle contract, not this script's
    # preference: validate-evidence.py requires an object with renderer,
    # terminal, and a named snapshots array whose dimensions match the PTY
    # metadata. See .campaign/skills/vhs-private-gist/references/evidence-manifest.md.
    terminal = {
        "columns": COLS,
        "rows": ROWS,
        "pixel_width": 1000,
        "pixel_height": 640,
        "mode": "dark/adaptive truecolor",
    }
    with open(os.path.join(evidence, "pty-transcript.txt"), "w") as fh:
        fh.write("\n".join(transcript) + "\n")
    with open(os.path.join(evidence, "screen-snapshots.json"), "w") as fh:
        json.dump({
            "renderer": "pyte",
            "terminal": terminal,
            "snapshots": snapshots,
        }, fh, indent=2)
    with open(os.path.join(evidence, "pty-metadata.json"), "w") as fh:
        json.dump({
            "transport": "pty",
            "renderer": "pyte",
            "pyte_version": importlib.metadata.version("pyte"),
            "fake_home": True,
            "fixture_id": "camp-machine-tailscale-check-v1",
            "terminal": terminal,
        }, fh, indent=2)

    for failure in failures:
        print("FAIL: %s" % failure, file=sys.stderr)
    if failures:
        return 1
    print("machine-tailscale-check: %d snapshots, link intact and copied over OSC 52" % len(snapshots))
    return 0


if __name__ == "__main__":
    sys.exit(main())
