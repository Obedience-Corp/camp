#!/usr/bin/env python3
"""PTY checks for internal/tui/selector via the compiled selector_demo.

Drives the real bubbletea widget in a terminal emulator (pyte) and reads the
screen a user would see: recency cursor, type-to-filter, no-match, and
esc/backspace. Unit tests cannot catch a missing cursor or a clipped query row.

Run: just tui pty-selector
"""
import fcntl
import os
import pty
import select
import struct
import sys
import termios
import time

import pyte

ROWS, COLS = 24, 80
KEYS = {
    "ENTER": "\r",
    "DOWN": "\x1b[B",
    "UP": "\x1b[A",
    "ESC": "\x1b",
    "BACKSPACE": "\x7f",
    "CTRLC": "\x03",
}
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 3.5


class Session:
    def __init__(self, binary):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.transcript = ""
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execve(binary, [binary], {
                "HOME": os.environ.get("HOME", "/tmp"),
                "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
                "TERM": "xterm-256color",
                "LINES": str(ROWS),
                "COLUMNS": str(COLS),
                "NO_COLOR": "1",
            })
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
            self.transcript += data.decode("utf-8", "replace")
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


FAILURES = []


def check(name, condition, detail=""):
    status = "PASS" if condition else "FAIL"
    print(f"[{status}] {name}" + (f"\n        {detail}" if detail and not condition else ""))
    if not condition:
        FAILURES.append(name)


def main():
    if len(sys.argv) != 2:
        print("usage: selector_pty.py <selector-demo-binary>", file=sys.stderr)
        sys.exit(2)
    binary = sys.argv[1]

    session = Session(binary)
    launch = session.display
    check("launch: title renders", "Select:" in launch, launch)
    check("launch: recency winner gamma is on screen", "gamma" in launch, launch)
    check("launch: cursor sits on gamma (bottom-proximity)", "> gamma" in launch or "▸ gamma" in launch, launch)
    check("launch: one help line", "type to filter" in launch, launch)
    check("launch: no stacked duplicate help", launch.count("type to filter") == 1, launch)

    session.press("b")
    filtered = session.display
    check("filter b: query row visible", "filter: b" in filtered, filtered)
    check("filter b: beta remains", "beta" in filtered, filtered)
    check("filter b: alpha dropped", "alpha" not in filtered, filtered)

    session.press("xyz")
    nomatch = session.display
    check("no-match: message renders", "no matches for" in nomatch, nomatch)
    check("no-match: query still shown", "filter:" in nomatch, nomatch)

    session.press("ESC")
    cleared = session.display
    check("esc: leaves filter", "filter:" not in cleared, cleared)
    check("esc: full list restored", "alpha" in cleared and "gamma" in cleared, cleared)

    session.press("DOWN")
    moved = session.display
    check("down: still rendering items", "alpha" in moved or "beta" in moved or "gamma" in moved, moved)

    session.press("BACKSPACE")
    session.close()

    if FAILURES:
        print(f"\n{len(FAILURES)} failed")
        sys.exit(1)
    print("\nall selector pty checks passed")


if __name__ == "__main__":
    main()
