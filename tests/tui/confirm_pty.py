#!/usr/bin/env python3
"""PTY checks for the fresh merged-branch Promote/Skip confirm via confirm_demo.

huh's base theme told the two buttons apart only by swapping two grays, and a
NO_COLOR terminal stripped even that, so a user could not see which choice
Enter would take. This drives the real form in a terminal emulator (pyte) under
NO_COLOR and reads the screen: the focused button must carry a text marker,
the marker must follow focus, and Enter must return the marked choice.

Run: just tui pty-confirm
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

ROWS, COLS = 24, 100
MARKER = "▸"
KEYS = {"ENTER": "\r", "LEFT": "\x1b[D", "RIGHT": "\x1b[C", "CTRLC": "\x03"}
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 3.5


class Session:
    def __init__(self, binary):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
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

    def press(self, key):
        os.write(self.fd, KEYS[key].encode())
        self.alive = self._drain(KEY_BUDGET)

    @property
    def display(self):
        return "\n".join(self.screen.display)

    def close(self):
        try:
            os.write(self.fd, KEYS["CTRLC"].encode())
            time.sleep(0.2)
        except OSError:
            pass
        for closer in (lambda: os.close(self.fd), lambda: os.waitpid(self.pid, os.WNOHANG)):
            try:
                closer()
            except (OSError, ChildProcessError):
                pass


FAILURES = []


def check(name, condition, detail=""):
    status = "PASS" if condition else "FAIL"
    print(f"[{status}] {name}" + (f"\n        {detail}" if detail and not condition else ""))
    if not condition:
        FAILURES.append(name)


def button_row(display):
    for line in display.splitlines():
        if "Promote" in line and "Skip" in line:
            return line
    return ""


def check_marker_follows_focus(binary):
    """The form opens on Skip (nothing is promoted without an explicit choice)."""
    session = Session(binary)
    row = button_row(session.display)
    check("launch: both buttons render on one row", row != "", session.display)
    check("launch: Skip is marked as the default choice", f"{MARKER} Skip" in row, row)
    check("launch: Promote is not marked", f"{MARKER} Promote" not in row, row)

    session.press("RIGHT")
    row = button_row(session.display)
    check("right: Promote becomes the marked choice", f"{MARKER} Promote" in row, row)
    check("right: Skip loses the marker", f"{MARKER} Skip" not in row, row)

    session.press("LEFT")
    row = button_row(session.display)
    check("left: Skip is marked again", f"{MARKER} Skip" in row, row)
    session.close()


def check_enter_returns_marked_choice(binary):
    session = Session(binary)
    row = button_row(session.display)
    check("enter: Skip is marked before submit", f"{MARKER} Skip" in row, row)
    session.press("ENTER")
    session._drain(1.0)
    transcript = session.transcript
    session.close()
    check("enter: submitting the marked Skip returns false", "RESULT:false" in transcript, transcript[-600:])

    session = Session(binary)
    session.press("RIGHT")
    row = button_row(session.display)
    check("enter: Promote is marked before submit", f"{MARKER} Promote" in row, row)
    session.press("ENTER")
    session._drain(1.0)
    transcript = session.transcript
    session.close()
    check("enter: submitting the marked Promote returns true", "RESULT:true" in transcript, transcript[-600:])


def main():
    if len(sys.argv) != 2:
        print("usage: confirm_pty.py <confirm-demo-binary>", file=sys.stderr)
        sys.exit(2)
    binary = os.path.abspath(sys.argv[1])
    check_marker_follows_focus(binary)
    check_enter_returns_marked_choice(binary)
    print()
    if FAILURES:
        print(f"{len(FAILURES)} check(s) failed: " + ", ".join(FAILURES))
        sys.exit(1)
    print("all confirm pty checks passed")


if __name__ == "__main__":
    main()
