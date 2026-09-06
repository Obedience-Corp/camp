#!/usr/bin/env python3
"""pty evidence for the intent explorer's copy-id action.

Unit tests can prove the handler sets a status string. They cannot prove the
operator ever sees it, and they cannot prove anything left camp. This drives
the real binary in a terminal and asserts both halves separately:

  render  the footer names the exact id, and does so in the success color
          rather than the error color every explorer status used to get
  write   the platform clipboard tool was handed that same id

It runs against a disposable fixture camp with fixed intent ids and stub
clipboard tools, so it never touches the operator's camp registry or clipboard.

Writes the evidence bundle files validate-evidence.py consumes:

    <evidence-dir>/pty-transcript.txt
    <evidence-dir>/pty-metadata.json
    <evidence-dir>/screen-snapshots.json

Usage: intent_copy_pty.py <fixture-dir> <evidence-dir>
"""
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

ROWS, COLS = 30, 130

# Fixed by the fixture recipe so the assertion is an exact match, not a prefix.
FIRST_ID = "clipboard-handoff-20260101-090000"

# camp can stall around five seconds at bubbletea init on a mute pty (a known
# tty-query issue, fixed in fest and still present here).
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 2.0


class Session:
    """One run of camp intent explore on a pty against the copy fixture."""

    def __init__(self, fixture, args, color=False):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.transcript = []
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(os.path.join(fixture, "camp"))
            binary = os.path.join(fixture, "bin", "camp")
            env = {
                "HOME": os.path.join(fixture, "home"),
                "PATH": os.path.join(fixture, "bin") + ":" + os.environ.get("PATH", "/usr/bin:/bin"),
                "TERM": "xterm-256color",
                "LINES": str(ROWS),
                "COLUMNS": str(COLS),
                "CAMP_VHS_HANDOFF_LOG": os.path.join(fixture, "handoff.log"),
            }
            if not color:
                env["NO_COLOR"] = "1"
            os.execve(binary, [binary] + args, env)
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

    def line_with(self, needle):
        """Return the buffer row index of the first line containing needle."""
        for row, line in enumerate(self.screen.display):
            if needle in line:
                return row
        return -1

    def foreground_of(self, row, needle):
        """The foreground color pyte recorded for needle on the given row.

        Read off the character cells rather than the escape stream: the footer
        is repainted, and the last write is the one the operator sees.
        """
        line = self.screen.display[row]
        start = line.find(needle)
        if start < 0:
            return None
        return self.screen.buffer[row][start].fg

    def close(self):
        # Snapshots are already taken, so tear down rather than quitting
        # politely: writing q to a pty nobody is reading and then blocking in
        # waitpid hangs this script rather than ending it.
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


def expected_clipboard_tool():
    """The clipboard command camp calls on THIS platform.

    Mirrors ui.clipboardCandidates. The fixture installs every name, so
    asserting the host-specific one remains an exact check.
    """
    if sys.platform == "darwin":
        return "pbcopy"
    if os.environ.get("WAYLAND_DISPLAY"):
        return "wl-copy"
    return "xclip"


def whole_line_with(display, needle):
    return any(needle in line for line in display)


def main():
    fixture, evidence = sys.argv[1], sys.argv[2]
    os.makedirs(evidence, exist_ok=True)
    handoff = os.path.join(fixture, "handoff.log")
    open(handoff, "w").close()

    failures = []
    snapshots = []
    transcript = []

    # ---- the copy itself, plus the group-header refusal ----
    s = Session(fixture, ["intent", "explore"])
    s.snapshot("explorer")

    # The cursor starts on a group header, where there is nothing to copy.
    s.press(b"y")
    header = s.snapshot("copy-on-group-header")
    if not whole_line_with(header, "No intent selected"):
        failures.append("y on a group header did not say there was nothing to copy")
    if whole_line_with(header, "Copied "):
        failures.append("y on a group header claimed a copy")

    # Move onto the first intent and yank it.
    s.press(b"j")
    s.press(b"y")
    copied = s.snapshot("copied")

    if not whole_line_with(copied, "Copied " + FIRST_ID):
        failures.append("footer does not report copying %s" % FIRST_ID)

    snapshots.extend(s.snapshots)
    transcript.extend(s.transcript)
    s.close()

    # ---- the help overlay documents the binding ----
    s = Session(fixture, ["intent", "explore"])
    s.press(b"?")
    s.snapshot("help-overlay")
    # The overlay is a viewport over content taller than the terminal, so the
    # QUICK ACTIONS block is below the fold on first paint. Scroll until the
    # binding appears rather than asserting on the first screen.
    found_binding = False
    for _ in range(12):
        if whole_line_with(s.screen.display, "Copy intent id to clipboard"):
            found_binding = True
            break
        s.press(b"\x04", 0.5)  # ctrl+d: half page down
    s.snapshot("help-overlay-quick-actions")
    if not found_binding:
        failures.append("help overlay does not document the copy binding")
    snapshots.extend(s.snapshots)
    transcript.extend(s.transcript)
    s.close()

    # ---- the color, which is the whole point of the severity change ----
    # Run again with color enabled: NO_COLOR would make success and error
    # indistinguishable and the check vacuous.
    s = Session(fixture, ["intent", "explore"], color=True)
    s.press(b"j")
    s.press(b"y")
    s.snapshot("copied-in-color")
    success_row = s.line_with("Copied " + FIRST_ID)
    success_fg = s.foreground_of(success_row, "Copied") if success_row >= 0 else None

    # a opens the dungeon-reason input; submitting it empty is refused. It is
    # the cheapest real error to reach from this screen and it mutates nothing.
    # Esc back to the list, because the reason input replaces the whole view
    # and the status line only renders in the list footer.
    s.press(b"a")
    s.press(b"\r")
    s.press(b"\x1b")
    s.snapshot("refused-in-color")
    error_row = s.line_with("Reason is required")
    error_fg = s.foreground_of(error_row, "Reason is required") if error_row >= 0 else None

    if success_fg is None:
        failures.append("could not read the copy confirmation's color")
    elif error_fg is None:
        failures.append("could not read an error message's color to compare against")
    elif success_fg == error_fg:
        failures.append(
            "the copy confirmation renders in the error color (%s); "
            "success and failure are indistinguishable" % success_fg
        )

    snapshots.extend(s.snapshots)
    transcript.extend(s.transcript)
    s.close()

    # ---- the write, which no recording can assert ----
    with open(handoff) as fh:
        handed = [line.strip() for line in fh if line.strip()]
    # Transcript only, never the snapshots array: these lines were read from a
    # file, and screen-snapshots.json is a record of what pyte rendered.
    transcript.append("===== handoff.log =====")
    transcript.extend(handed)

    tool = expected_clipboard_tool()
    if ("%s %s" % (tool, FIRST_ID)) not in handed:
        failures.append("%s was not handed the exact id: %r" % (tool, handed))
    if any(line.split(" ", 1)[0] == tool and line != "%s %s" % (tool, FIRST_ID) for line in handed):
        failures.append("clipboard received something other than the id: %r" % handed)

    # The shapes here are the evidence-bundle contract, not this script's
    # preference: validate-evidence.py requires an object with renderer,
    # terminal, and a named snapshots array whose dimensions match the PTY
    # metadata. See .campaign/skills/vhs-private-gist/references/evidence-manifest.md.
    terminal = {
        "columns": COLS,
        "rows": ROWS,
        "pixel_width": 1180,
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
            "fixture_id": "camp-intent-copy-v1",
            "terminal": terminal,
        }, fh, indent=2)

    for failure in failures:
        print("FAIL: %s" % failure, file=sys.stderr)
    if failures:
        return 1
    print("intent-copy: %d snapshots, %s handed the exact id" % (len(snapshots), tool))
    return 0


if __name__ == "__main__":
    sys.exit(main())
