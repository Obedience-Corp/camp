#!/usr/bin/env python3
"""pty checks for the triage review flow.

Green unit tests do not verify a TUI, and a VHS recording cannot assert what
landed on disk. This drives the real compiled binary on a pty, renders its
output through a terminal emulator, and then checks the run files — the screen
and the write are separate claims and both are made here.

Three assertions, from spec doc 07:

  1. after [y], the verdict exists in decisions.jsonl (the file, not the screen)
  2. kill and reopen resumes at the first undecided row
  3. a terminal row inside a lane is NOT covered by a lane approval

Run it through the justfile:  just tui triage-review

Requires pyte. The justfile target provisions a throwaway venv for it; this
script only needs `import pyte` to succeed.
"""
import fcntl
import json
import os
import pty
import select
import struct
import subprocess
import sys
import termios
import time

import pyte

ROWS, COLS = 44, 130

KEYS = {"ENTER": "\r", "DOWN": "\x1b[B", "UP": "\x1b[A", "ESC": "\x1b", "CTRLC": "\x03"}

# camp can stall around five seconds at bubbletea init on a mute pty (a known
# tty-query issue, fixed in fest and still present here). The launch budget
# absorbs it so a slow first paint is not misread as a hang.
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 3.5


class Session:
    """One run of the review flow on a pty."""

    def __init__(self, campaign, home, binary):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(campaign)
            os.execve(binary, [binary, "triage", "review"], {
                "HOME": home,
                "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
                "TERM": "xterm-256color",
                "LINES": str(ROWS),
                "COLUMNS": str(COLS),
                "NO_COLOR": "1",
            })
        # Both the ioctl and LINES/COLUMNS: a small default clips content and
        # sends you chasing a layout bug that does not exist.
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.alive = self._drain(LAUNCH_BUDGET)
        self._snapshot("launch")

    def _drain(self, budget):
        deadline = time.time() + budget
        while time.time() < deadline:
            ready, _, _ = select.select([self.fd], [], [], 0.25)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError:
                return False
            if not data:
                return False
            self.stream.feed(data)
            deadline = time.time() + budget
        return True

    def _snapshot(self, label):
        self.snapshots.append((label, "\n".join(self.screen.display)))

    def press(self, *keys):
        for key in keys:
            if not self.alive:
                break
            os.write(self.fd, KEYS.get(key, key).encode())
            self.alive = self._drain(KEY_BUDGET)
            self._snapshot(f"key {key}")
        return self

    @property
    def display(self):
        return "\n".join(self.screen.display)

    def close(self):
        """Take the final snapshot before tearing down, never after."""
        self._snapshot("final")
        try:
            os.write(self.fd, KEYS["CTRLC"].encode())
            time.sleep(0.2)
        except OSError:
            pass
        for closer in (lambda: os.close(self.fd), lambda: os.waitpid(self.pid, os.WNOHANG)):
            try:
                closer()
            except OSError:
                pass
            except ChildProcessError:
                pass

    def dump(self):
        for label, body in self.snapshots:
            print(f"\n----- {label} -----")
            print(body.rstrip())


def decisions(campaign):
    """Read the run's verdict stream: the write, not the screen."""
    latest = os.path.join(campaign, ".campaign", "triage", "latest")
    with open(latest) as handle:
        run_id = handle.read().strip()
    path = os.path.join(campaign, ".campaign", "triage", "runs", run_id, "decisions.jsonl")
    if not os.path.exists(path):
        return []
    events = []
    with open(path) as handle:
        for line in handle:
            line = line.strip()
            if line:
                events.append(json.loads(line))
    return events


def verdict_events(campaign, stable_id, kinds=("approved", "amended", "rejected")):
    return [e for e in decisions(campaign)
            if e["stable_id"] == stable_id and e["event"] in kinds]


FAILURES = []


def check(name, condition, detail=""):
    status = "PASS" if condition else "FAIL"
    print(f"[{status}] {name}" + (f"\n        {detail}" if detail and not condition else ""))
    if not condition:
        FAILURES.append(name)


def fixture(binary):
    """Bootstrap a fresh fixture campaign and return (campaign, home)."""
    script = os.path.join(os.path.dirname(__file__), "..", "..",
                          "docs", "demos", "fixtures", "triage-review-fixture.sh")
    env = dict(os.environ, CAMP_BIN=binary)
    out = subprocess.run(["bash", os.path.abspath(script)], env=env,
                         capture_output=True, text=True, check=True).stdout
    values = {}
    for line in out.splitlines():
        if line.startswith("export "):
            key, _, value = line[len("export "):].partition("=")
            values[key] = value
    return values["TRIAGE_FIXTURE"], values["TRIAGE_FIXTURE_HOME"]


def check_approve_writes_the_verdict(binary):
    """1. After [y] the verdict is in decisions.jsonl, not merely on screen."""
    campaign, home = fixture(binary)
    before = verdict_events(campaign, "design-observation-boundary")

    session = Session(campaign, home, binary)
    session.press("ENTER", "ENTER", "ENTER")  # lane -> row -> approve
    rendered = session.display
    session.close()

    check("approve: the screen reports the verdict",
          "approved" in rendered, rendered)

    after = verdict_events(campaign, "design-observation-boundary")
    check("approve: the verdict is written to decisions.jsonl",
          len(after) == len(before) + 1 and after[-1]["event"] == "approved",
          f"events for the row: {after}")
    check("approve: the recorded action is the one the card showed",
          bool(after) and after[-1]["canonical_action"] == "attention/parked",
          f"events for the row: {after}")


def check_reopen_resumes(binary):
    """2. Kill and reopen resumes at the first undecided row."""
    campaign, home = fixture(binary)

    first = Session(campaign, home, binary)
    first.press("ENTER", "ENTER", "ENTER")  # approve the first parked row
    first.close()

    approved = verdict_events(campaign, "design-observation-boundary")
    check("resume: the first session's verdict survived the kill",
          len(approved) == 1, f"events: {approved}")

    second = Session(campaign, home, binary)
    second.press("ENTER")  # open the same lane
    rendered = second.display
    second.close()

    check("resume: the lane offers the row left undecided",
          "design-shared-templates" in rendered, rendered)
    check("resume: it does not re-offer the row already decided",
          "Open next row - design-observation-boundary" not in rendered, rendered)


def check_lane_approve_skips_terminal(binary):
    """3. A terminal row is not covered by a lane approval."""
    campaign, home = fixture(binary)

    session = Session(campaign, home, binary)
    # Summary -> parked lane -> approve whole lane -> confirm (second option).
    session.press("ENTER", "DOWN", "ENTER", "DOWN", "ENTER")
    rendered = session.display
    session.close()

    parked = [verdict_events(campaign, slug) for slug in
              ("design-observation-boundary", "design-shared-templates")]
    check("lane approve: both non-terminal rows were recorded",
          all(len(events) == 1 for events in parked), f"events: {parked}")

    terminal = verdict_events(campaign, "design-schema-tags")
    check("lane approve: the terminal row got NO verdict",
          terminal == [],
          f"a lane approval must never cover a terminal row, got: {terminal}")

    consolidate = verdict_events(campaign, "design-platform-adoption")
    check("lane approve: the consolidation got NO verdict",
          consolidate == [],
          f"a split is terminal too, got: {consolidate}")

    check("lane approve: the confirmation listed what it covered",
          "design-observation-boundary" in rendered, rendered)


def main():
    binary = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "bin/camp")
    if not os.path.exists(binary):
        sys.exit(f"camp binary not found at {binary}; run `just build-camp` first")

    print(f"driving {binary} on a pty ({ROWS}x{COLS})\n")
    check_approve_writes_the_verdict(binary)
    print()
    check_reopen_resumes(binary)
    print()
    check_lane_approve_skips_terminal(binary)

    print()
    if FAILURES:
        print(f"{len(FAILURES)} check(s) failed: {', '.join(FAILURES)}")
        sys.exit(1)
    print("all pty checks passed")


if __name__ == "__main__":
    main()
